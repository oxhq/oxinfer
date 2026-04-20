use std::collections::{BTreeMap, BTreeSet};

use crate::model::{
    BroadcastOut, BroadcastParameterOut, ControllerMethodOut, ControllerOut, Delta, DeltaMeta,
    ModelOut, ModelRelationshipOut, PivotOut, PolymorphicOut, PolymorphicRelationOut,
    RequestUsageOut, ResourceUsageOut, ScopeUsedOut, StatsOut,
};
use crate::pipeline::PipelineResult;

pub fn build_delta(result: PipelineResult) -> Delta {
    let scope_owners = collect_scope_owners(&result);
    let route_methods = collect_route_methods(&result);
    let mut controllers = BTreeMap::<(String, String), Vec<ControllerMethodOut>>::new();
    let mut models = BTreeMap::<(String, String), ModelOut>::new();
    let mut polymorphic = BTreeMap::<String, Vec<PolymorphicRelationOut>>::new();
    let mut broadcast = BTreeMap::<String, BroadcastOut>::new();

    for file in &result.files {
        for controller in &file.facts.controllers {
            let controller_key = (controller.fqcn.clone(), controller.method_name.clone());
            if !route_methods.is_empty() && !route_methods.contains_key(&controller_key) {
                continue;
            }

            let http_methods = route_methods
                .get(&controller_key)
                .cloned()
                .unwrap_or_default();
            let http_status = controller
                .http_status
                .map(|status| vec![status])
                .unwrap_or_else(|| {
                    if http_methods.is_empty() {
                        Vec::new()
                    } else {
                        vec![200]
                    }
                });

            controllers
                .entry((controller.fqcn.clone(), file.relative_path.clone()))
                .or_default()
                .push(ControllerMethodOut {
                    name: controller.method_name.clone(),
                    http_methods,
                    http_status,
                    request_usage: build_request_usage(controller),
                    resource_usage: controller
                        .resource_usage
                        .iter()
                        .map(|resource| ResourceUsageOut {
                            class: resource.class_name.clone(),
                            method: resource.method.clone(),
                        })
                        .collect(),
                    scopes_used: controller
                        .scopes_used
                        .iter()
                        .map(|scope| ScopeUsedOut {
                            name: scope.name.clone(),
                            on: scope
                                .on
                                .clone()
                                .or_else(|| scope_owners.get(&scope.name).cloned().flatten()),
                        })
                        .collect(),
                });
        }

        for model in &file.facts.models {
            let entry = models
                .entry((model.fqcn.clone(), file.relative_path.clone()))
                .or_insert_with(|| ModelOut {
                    fqcn: model.fqcn.clone(),
                    file: file.relative_path.clone(),
                    relationships: Vec::new(),
                    scopes: Vec::new(),
                    attributes: Vec::new(),
                    with_pivot: Vec::new(),
                });

            entry
                .relationships
                .extend(
                    model
                        .relationships
                        .iter()
                        .map(|relationship| ModelRelationshipOut {
                            name: Some(relationship.name.clone()),
                            relation_type: Some(relationship.relation_type.clone()),
                            related: relationship.related.clone(),
                            with_pivot: if relationship.pivot_columns.is_empty() {
                                Vec::new()
                            } else {
                                vec![PivotOut {
                                    relation: Some(relationship.name.clone()),
                                    columns: relationship.pivot_columns.clone(),
                                }]
                            },
                        }),
                );
            entry.scopes.extend(model.scopes.iter().cloned());
            entry.attributes.extend(model.attributes.iter().cloned());
        }

        for item in &file.facts.polymorphic {
            polymorphic
                .entry(item.name.clone())
                .or_default()
                .push(PolymorphicRelationOut {
                    model: item.model.clone(),
                    relation_type: item.relation.clone(),
                });
        }

        for item in &file.facts.broadcast {
            broadcast
                .entry(item.channel.clone())
                .or_insert_with(|| BroadcastOut {
                    channel: item.channel.clone(),
                    channel_type: item.channel_type.clone(),
                    parameters: if item.parameters.is_empty() {
                        extract_channel_parameters(&item.channel)
                    } else {
                        item.parameters
                            .iter()
                            .map(|parameter| BroadcastParameterOut {
                                name: parameter.name.clone(),
                                parameter_type: parameter.parameter_type.clone(),
                            })
                            .collect()
                    },
                });
        }
    }

    let mut controllers = controllers
        .into_iter()
        .map(|((fqcn, file), mut methods)| {
            methods.sort_by(|a, b| a.name.cmp(&b.name));
            ControllerOut {
                fqcn,
                file,
                methods,
            }
        })
        .collect::<Vec<_>>();
    controllers.sort_by(|a, b| (&a.fqcn, &a.file).cmp(&(&b.fqcn, &b.file)));

    let mut models = models
        .into_values()
        .map(|mut model| {
            model.relationships.sort_by(|a, b| {
                (&a.name, &a.relation_type, &a.related).cmp(&(
                    &b.name,
                    &b.relation_type,
                    &b.related,
                ))
            });
            model.relationships.dedup_by(|a, b| {
                a.name == b.name && a.relation_type == b.relation_type && a.related == b.related
            });
            model.scopes.sort();
            model.scopes.dedup();
            model.attributes.sort();
            model.attributes.dedup();
            for pivot in &mut model.with_pivot {
                pivot.columns.sort();
                pivot.columns.dedup();
            }
            model
                .with_pivot
                .sort_by(|a, b| (&a.relation, &a.columns).cmp(&(&b.relation, &b.columns)));
            model
                .with_pivot
                .dedup_by(|a, b| a.relation == b.relation && a.columns == b.columns);
            model
        })
        .collect::<Vec<_>>();
    models.sort_by(|a, b| (&a.fqcn, &a.file).cmp(&(&b.fqcn, &b.file)));

    let mut polymorphic = polymorphic
        .into_iter()
        .map(|(name, mut relations)| {
            relations
                .sort_by(|a, b| (&a.model, &a.relation_type).cmp(&(&b.model, &b.relation_type)));
            relations.dedup_by(|a, b| a.model == b.model && a.relation_type == b.relation_type);
            let discriminator = result
                .files
                .iter()
                .flat_map(|file| file.facts.polymorphic.iter())
                .find(|item| item.name == name)
                .map(|item| item.discriminator.clone());
            PolymorphicOut {
                name: Some(name),
                discriminator,
                relations,
            }
        })
        .collect::<Vec<_>>();
    polymorphic.sort_by(|a, b| (&a.name, &a.discriminator).cmp(&(&b.name, &b.discriminator)));

    let broadcast = broadcast.into_values().collect::<Vec<_>>();

    Delta {
        meta: DeltaMeta {
            partial: result.partial,
            stats: StatsOut {
                files_parsed: result.files.len(),
                skipped: 0,
                duration_ms: result.duration_ms,
            },
        },
        controllers,
        models,
        polymorphic,
        broadcast,
    }
}

fn collect_scope_owners(result: &PipelineResult) -> BTreeMap<String, Option<String>> {
    let mut owners = BTreeMap::<String, BTreeSet<String>>::new();

    for file in &result.files {
        for model in &file.facts.models {
            for scope in &model.scopes {
                owners
                    .entry(scope.clone())
                    .or_default()
                    .insert(model.fqcn.clone());
            }
        }
    }

    owners
        .into_iter()
        .map(|(scope, models)| {
            let owner = if models.len() == 1 {
                models.into_iter().next()
            } else {
                None
            };
            (scope, owner)
        })
        .collect()
}

fn build_request_usage(controller: &crate::model::ControllerMethod) -> Vec<RequestUsageOut> {
    let mut usage = controller
        .request_usage
        .iter()
        .map(|item| {
            let mut rules = item.rules.clone();
            let mut fields = item.fields.clone();
            rules.sort();
            rules.dedup();
            fields.sort();
            fields.dedup();
            RequestUsageOut {
                method: item.method.clone(),
                class: item.class_name.clone(),
                rules,
                fields,
                location: item.location.clone(),
            }
        })
        .collect::<Vec<_>>();

    usage.sort_by(|a, b| {
        (&a.method, &a.class, &a.location, &a.rules, &a.fields).cmp(&(
            &b.method,
            &b.class,
            &b.location,
            &b.rules,
            &b.fields,
        ))
    });
    usage.dedup_by(|a, b| {
        a.method == b.method
            && a.class == b.class
            && a.location == b.location
            && a.rules == b.rules
            && a.fields == b.fields
    });
    usage
}

fn collect_route_methods(result: &PipelineResult) -> BTreeMap<(String, String), Vec<String>> {
    let mut route_methods = BTreeMap::<(String, String), BTreeSet<String>>::new();

    for binding in &result.route_bindings {
        route_methods
            .entry((binding.controller_fqcn.clone(), binding.method_name.clone()))
            .or_default()
            .extend(binding.http_methods.iter().cloned());
    }

    route_methods
        .into_iter()
        .map(|(key, values)| (key, values.into_iter().collect()))
        .collect()
}

fn extract_channel_parameters(channel: &str) -> Vec<BroadcastParameterOut> {
    let mut parameters = Vec::new();
    let mut seen = BTreeSet::new();
    let mut current = String::new();
    let mut inside = false;

    for ch in channel.chars() {
        match ch {
            '{' if !inside => {
                inside = true;
                current.clear();
            }
            '}' if inside => {
                inside = false;
                if !current.is_empty() && seen.insert(current.clone()) {
                    parameters.push(BroadcastParameterOut {
                        name: current.clone(),
                        parameter_type: None,
                    });
                }
                current.clear();
            }
            _ if inside => current.push(ch),
            _ => {}
        }
    }

    parameters
}
