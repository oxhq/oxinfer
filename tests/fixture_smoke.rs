use std::fs;
use std::path::{Path, PathBuf};

use oxinfer::manifest::Manifest;
use oxinfer::pipeline::run_pipeline;

fn load_manifest(path: &Path) -> Manifest {
    let text = fs::read_to_string(path).expect("fixture manifest should exist");
    let mut manifest: Manifest =
        serde_json::from_str(&text).expect("fixture manifest should decode");
    manifest.resolve_paths(path);
    manifest
}

fn fixture_path(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("fixtures")
        .join(format!("{name}.manifest.json"))
}

#[test]
fn minimal_fixture_produces_controllers_and_models() {
    let delta =
        run_pipeline(&load_manifest(&fixture_path("minimal"))).expect("pipeline should succeed");

    assert_eq!(delta.meta.stats.files_parsed, 5);
    assert_eq!(delta.meta.stats.skipped, 0);
    assert_eq!(delta.controllers.len(), 1);
    assert_eq!(delta.models.len(), 2);
    assert!(delta.broadcast.is_empty());
    assert!(delta.polymorphic.is_empty());

    let users = delta
        .controllers
        .iter()
        .find(|controller| controller.fqcn == "App\\Http\\Controllers\\UserController")
        .expect("user controller should exist");
    assert_eq!(users.file, "app/Http/Controllers/UserController.php");
    assert_eq!(users.methods.len(), 5);

    let store = users
        .methods
        .iter()
        .find(|method| method.name == "store")
        .expect("store method should exist");
    assert_eq!(store.http_methods, vec!["POST"]);
    assert_eq!(store.http_status, vec![201]);
    let validation = store
        .request_usage
        .iter()
        .find(|usage| usage.method == "validate")
        .expect("store should include validate usage");
    assert!(validation.rules.contains(&"name".to_string()));
    assert!(validation.rules.contains(&"email".to_string()));
    assert!(
        !validation
            .rules
            .contains(&"required|string|max:255".to_string())
    );
}

#[test]
fn api_fixture_detects_resources_and_scopes() {
    let delta =
        run_pipeline(&load_manifest(&fixture_path("api"))).expect("pipeline should succeed");

    assert_eq!(delta.meta.stats.files_parsed, 13);
    assert_eq!(delta.controllers.len(), 1);
    assert_eq!(delta.models.len(), 5);

    let product_controller = delta
        .controllers
        .iter()
        .find(|controller| controller.fqcn == "App\\Http\\Controllers\\ProductController")
        .expect("product controller should exist");
    assert_eq!(product_controller.methods.len(), 6);

    let featured = product_controller
        .methods
        .iter()
        .find(|method| method.name == "featured")
        .expect("featured method should exist");
    assert_eq!(featured.http_methods, vec!["GET"]);
    assert_eq!(featured.http_status, vec![200]);
    assert!(featured.resource_usage.iter().any(|resource| resource.class
        == "App\\Http\\Resources\\ProductResource"
        && resource.method.as_deref() == Some("collection")));

    let store = product_controller
        .methods
        .iter()
        .find(|method| method.name == "store")
        .expect("store method should exist");
    assert!(
        store
            .request_usage
            .iter()
            .any(|usage| usage.method == "validated"
                && usage.class.as_deref() == Some("App\\Http\\Requests\\StoreProductRequest"))
    );
    assert!(store.resource_usage.iter().any(|resource| resource.class
        == "App\\Http\\Resources\\ProductResource"
        && resource.method.as_deref() == Some("response")));

    let product = delta
        .models
        .iter()
        .find(|model| model.fqcn == "App\\Models\\Product")
        .expect("product model should exist");
    assert_eq!(product.file, "app/Models/Product.php");
    assert!(product.scopes.contains(&"active".to_string()));
    assert!(product.scopes.contains(&"featured".to_string()));
}

#[test]
fn complex_fixture_detects_project_level_features() {
    let delta =
        run_pipeline(&load_manifest(&fixture_path("complex"))).expect("pipeline should succeed");

    assert_eq!(delta.meta.stats.files_parsed, 10);
    assert_eq!(delta.controllers.len(), 1);
    assert_eq!(delta.broadcast.len(), 16);
    assert!(!delta.polymorphic.is_empty());
    assert!(
        delta
            .broadcast
            .iter()
            .any(|channel| channel.channel_type.as_deref() == Some("public"))
    );
    assert!(
        delta
            .broadcast
            .iter()
            .any(|channel| channel.channel_type.as_deref() == Some("private"))
    );
    assert!(
        delta
            .broadcast
            .iter()
            .any(|channel| channel.channel_type.as_deref() == Some("presence"))
    );

    let post_controller = delta
        .controllers
        .iter()
        .find(|controller| controller.fqcn == "App\\Http\\Controllers\\PostController")
        .expect("post controller should exist");

    let index = post_controller
        .methods
        .iter()
        .find(|method| method.name == "index")
        .expect("index method should exist");
    assert_eq!(index.http_methods, vec!["GET"]);
    assert_eq!(index.http_status, vec![200]);
    assert!(
        index
            .scopes_used
            .iter()
            .any(|scope| scope.name == "byActiveUsers"
                && scope.on.as_deref() == Some("App\\Models\\Post"))
    );
    assert!(
        index
            .scopes_used
            .iter()
            .any(|scope| scope.name == "withTags"
                && scope.on.as_deref() == Some("App\\Models\\Post"))
    );

    let store = post_controller
        .methods
        .iter()
        .find(|method| method.name == "store")
        .expect("store method should exist");
    assert_eq!(store.http_status, vec![201]);

    let morph_many = delta
        .polymorphic
        .iter()
        .find(|group| group.name.as_deref() == Some("commentable"))
        .expect("commentable group should exist");
    assert!(
        morph_many
            .relations
            .iter()
            .any(|relation| relation.model == "App\\Models\\Post")
    );
    assert!(
        morph_many
            .relations
            .iter()
            .any(|relation| relation.model == "App\\Models\\Video")
    );

    let workspace = delta
        .broadcast
        .iter()
        .find(|channel| channel.channel == "workspace.{workspaceId}.team.{teamId}")
        .expect("workspace channel should exist");
    assert_eq!(workspace.parameters.len(), 2);
    assert_eq!(workspace.parameters[0].name, "workspaceId");
    assert_eq!(workspace.parameters[1].name, "teamId");
}
