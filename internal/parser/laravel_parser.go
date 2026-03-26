package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	
	"github.com/oxhq/oxinfer/internal/logging"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

type LaravelParser struct {
	parser      *sitter.Parser
	language    *sitter.Language
	ModelScopes map[string][]string
	Controllers map[string][]string
	Models      map[string]ModelInfo
	verbose     bool
}

// ModelInfo contains everything we need about a Laravel model.
type ModelInfo struct {
	FQCN       string
	FilePath   string
	Scopes     []string
	Relations  []string
	Attributes []string
}

// ControllerInfo contains everything we need about a Laravel controller.
type ControllerInfo struct {
	FQCN     string
	FilePath string
	Methods  []string
}

// ParsedData is what we actually need from parsing.
type ParsedData struct {
	Controllers []ControllerInfo
	Models      []ModelInfo
	Routes      []RouteInfo
}

// RouteInfo contains route definitions.
type RouteInfo struct {
	Method     string
	Path       string
	Controller string
	Action     string
}

// NewLaravelParser creates a parser for Laravel projects.
func NewLaravelParser(verbose bool) (*LaravelParser, error) {
	parser := sitter.NewParser()
	if parser == nil {
		return nil, fmt.Errorf("failed to create parser")
	}
	
	language := php.GetLanguage()
	if language == nil {
		return nil, fmt.Errorf("failed to load PHP language")
	}
	
	parser.SetLanguage(language)
	
	return &LaravelParser{
		parser:      parser,
		language:    language,
		ModelScopes: make(map[string][]string),
		Controllers: make(map[string][]string),
		Models:      make(map[string]ModelInfo),
		verbose:     verbose,
	}, nil
}

// ParseFile parses a PHP file, extracts Laravel patterns, and returns the syntax tree for reuse.
func (p *LaravelParser) ParseFile(ctx context.Context, filePath string) (*SyntaxTree, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	tree, err := p.parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}
	// Don't close the tree here - we'll return it for reuse
	
	root := tree.RootNode()
	
	// Route to appropriate processor
	className := p.findClassName(root, content)
	namespace := p.findNamespace(root, content)
	extends := p.findExtends(root, content)

	// AST-based gating
	if p.extendsEloquentModel(extends) {
		logging.VerboseFromContext(ctx, "parser", "Processing MODEL: %s", filePath)
		logging.DebugFromContext(ctx, "parser", "processing model file", map[string]interface{}{
			"file_path": filePath,
			"file_type": "model",
		})
		p.extractModel(ctx, root, content, filePath)
	} else if p.isControllerByAST(namespace, className, extends) {
		logging.VerboseFromContext(ctx, "parser", "Processing CONTROLLER: %s", filePath)
		logging.DebugFromContext(ctx, "parser", "processing controller file", map[string]interface{}{
			"file_path": filePath,
			"file_type": "controller",
		})
		p.extractController(ctx, root, content, filePath)
	} else {
		logging.VerboseFromContext(ctx, "parser", "SKIPPING (not model/controller): %s", filePath)
		logging.DebugFromContext(ctx, "parser", "skipping file", map[string]interface{}{
			"file_path": filePath,
			"reason":    "not model or controller",
		})
	}
	
	// Wrap in SyntaxTree format and return for reuse
	return &SyntaxTree{
		Root: &SyntaxNode{
			Type:       root.Type(),
			StartByte:  int(root.StartByte()),
			EndByte:    int(root.EndByte()),
			StartPoint: Point{Row: int(root.StartPoint().Row), Column: int(root.StartPoint().Column)},
			EndPoint:   Point{Row: int(root.EndPoint().Row), Column: int(root.EndPoint().Column)},
		},
		Source: content, // Keep as []byte to match interface
	}, nil
}

func (p *LaravelParser) extractModel(ctx context.Context, root *sitter.Node, content []byte, filePath string) {
	className := p.findClassName(root, content)
	if className == "" {
		return
	}
	
	namespace := p.findNamespace(root, content)
	fqcn := className
	if namespace != "" {
		fqcn = namespace + "\\" + className
	}
	
	var scopes []string
	var relations []string
	
	p.walkNode(root, content, func(node *sitter.Node) bool {
		if node.Type() == "method_declaration" {
			methodName := p.getMethodName(node, content)
			
			if strings.HasPrefix(methodName, "scope") && len(methodName) > 5 {
				scopeName := methodName[5:]
				scopeName = strings.ToLower(scopeName[:1]) + scopeName[1:]
				scopes = append(scopes, scopeName)
				
				if p.verbose {
					fmt.Printf("Found custom scope: %s::%s() -> %s\n", fqcn, methodName, scopeName)
				}
			}
			
			if p.isRelation(methodName) {
				relations = append(relations, methodName)
			}
		}
		return true
	})
	
	p.Models[fqcn] = ModelInfo{
		FQCN:      fqcn,
		FilePath:  filePath,
		Scopes:    scopes,
		Relations: relations,
	}
	
	if len(scopes) > 0 {
		p.ModelScopes[fqcn] = scopes
		if p.verbose {
			fmt.Printf("Model %s registered with %d custom scopes: %v\n", fqcn, len(scopes), scopes)
		}
	}
}

// extractController gets what we need from a controller file.
func (p *LaravelParser) extractController(ctx context.Context, root *sitter.Node, content []byte, filePath string) {
	className := p.findClassName(root, content)
	if className == "" {
		logging.VerboseFromContext(ctx, "parser", "Controller %s: NO CLASS NAME FOUND", filePath)
		logging.DebugFromContext(ctx, "parser", "controller class name not found", map[string]interface{}{
			"file_path": filePath,
		})
		return
	}
	
	namespace := p.findNamespace(root, content)
	extends := p.findExtends(root, content)
	fqcn := className
	if namespace != "" {
		fqcn = namespace + "\\" + className
	}
	
	logging.VerboseFromContext(ctx, "parser", "Controller %s: class=%s, fqcn=%s", filePath, className, fqcn)
	logging.DebugFromContext(ctx, "parser", "controller parsed", map[string]interface{}{
		"file_path":  filePath,
		"class_name": className,
		"fqcn":       fqcn,
		"namespace":  namespace,
	})
	
	// Find public methods (actions), falling back to inherited controller actions
	// for thin concrete controllers that delegate everything to an abstract parent.
	methods := p.extractPublicControllerMethods(root, content)
	if len(methods) == 0 && extends != "" {
		methods = p.resolveInheritedControllerMethods(ctx, filePath, extends, map[string]struct{}{})
	}
	
	logging.VerboseFromContext(ctx, "parser", "Controller %s: found %d methods: %v", filePath, len(methods), methods)
	logging.DebugFromContext(ctx, "parser", "controller methods found", map[string]interface{}{
		"file_path":    filePath,
		"fqcn":         fqcn,
		"method_count": len(methods),
		"methods":      methods,
	})
	
	if len(methods) > 0 {
		p.Controllers[fqcn] = methods
		logging.VerboseFromContext(ctx, "parser", "Controller %s: STORED in registry as %s", filePath, fqcn)
		logging.DebugFromContext(ctx, "parser", "controller registered", map[string]interface{}{
			"file_path": filePath,
			"fqcn":      fqcn,
			"stored":    true,
		})
	} else {
		logging.VerboseFromContext(ctx, "parser", "Controller %s: NO METHODS - not stored", filePath)
		logging.DebugFromContext(ctx, "parser", "controller not registered", map[string]interface{}{
			"file_path": filePath,
			"fqcn":      fqcn,
			"reason":    "no methods found",
		})
	}
}

func (p *LaravelParser) extractPublicControllerMethods(root *sitter.Node, content []byte) []string {
	var methods []string
	seen := make(map[string]struct{})

	p.walkNode(root, content, func(node *sitter.Node) bool {
		if node.Type() != "method_declaration" {
			return true
		}

		for i := uint32(0); i < node.ChildCount(); i++ {
			child := node.Child(int(i))
			if child == nil || child.Type() != "visibility_modifier" {
				continue
			}

			visibility := string(content[child.StartByte():child.EndByte()])
			if visibility != "public" {
				break
			}

			methodName := p.getMethodName(node, content)
			if methodName == "" || (methodName != "__invoke" && strings.HasPrefix(methodName, "__")) {
				break
			}
			if _, exists := seen[methodName]; exists {
				break
			}

			seen[methodName] = struct{}{}
			methods = append(methods, methodName)
			break
		}

		return true
	})

	return methods
}

func (p *LaravelParser) resolveInheritedControllerMethods(ctx context.Context, filePath, extends string, visited map[string]struct{}) []string {
	parentPath := p.resolveSiblingControllerPath(filePath, extends)
	if parentPath == "" {
		return nil
	}

	absParentPath, err := filepath.Abs(parentPath)
	if err != nil {
		absParentPath = parentPath
	}
	if _, seen := visited[absParentPath]; seen {
		return nil
	}
	visited[absParentPath] = struct{}{}

	content, err := os.ReadFile(absParentPath)
	if err != nil {
		return nil
	}

	tree, err := p.parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil
	}

	root := tree.RootNode()
	methods := p.extractPublicControllerMethods(root, content)
	if len(methods) > 0 {
		return methods
	}

	parentExtends := p.findExtends(root, content)
	if parentExtends == "" {
		return nil
	}

	return p.resolveInheritedControllerMethods(ctx, absParentPath, parentExtends, visited)
}

func (p *LaravelParser) resolveSiblingControllerPath(filePath, extends string) string {
	parentClass := strings.TrimSpace(extends)
	if parentClass == "" {
		return ""
	}

	if idx := strings.LastIndex(parentClass, `\`); idx >= 0 {
		parentClass = parentClass[idx+1:]
	}
	if parentClass == "" {
		return ""
	}

	candidate := filepath.Join(filepath.Dir(filePath), parentClass+".php")
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}

	return candidate
}

// Helper methods

func (p *LaravelParser) findClassName(root *sitter.Node, content []byte) string {
	var className string
	p.walkNode(root, content, func(node *sitter.Node) bool {
		if node.Type() == "class_declaration" {
			for i := uint32(0); i < node.ChildCount(); i++ {
				child := node.Child(int(i))
				if child.Type() == "name" {
					className = string(content[child.StartByte():child.EndByte()])
					return false // Stop walking
				}
			}
		}
		return true
	})
	return className
}

func (p *LaravelParser) findNamespace(root *sitter.Node, content []byte) string {
	var namespace string
	p.walkNode(root, content, func(node *sitter.Node) bool {
		if node.Type() == "namespace_definition" {
			for i := uint32(0); i < node.ChildCount(); i++ {
				child := node.Child(int(i))
				if child.Type() == "namespace_name" {
					namespace = string(content[child.StartByte():child.EndByte()])
					return false // Stop walking
				}
			}
		}
		return true
	})
	return namespace
}

func (p *LaravelParser) getMethodName(methodNode *sitter.Node, content []byte) string {
	for i := uint32(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(int(i))
		if child.Type() == "name" {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	return ""
}

func (p *LaravelParser) walkNode(node *sitter.Node, content []byte, visitor func(*sitter.Node) bool) {
	if !visitor(node) {
		return
	}
	
	for i := uint32(0); i < node.ChildCount(); i++ {
		p.walkNode(node.Child(int(i)), content, visitor)
	}
}

func (p *LaravelParser) isModel(filePath string) bool {
	// Be strict to avoid non-Eloquent contamination. Prefer standard locations only.
	if strings.Contains(filePath, "app/Models/") {
		return true
	}
	// Also allow vendor/package style explicit Models directories
	if strings.Contains(filePath, "/Models/") || strings.Contains(filePath, "src/Models/") {
		return true
	}
	return false
}

func (p *LaravelParser) isController(filePath string) bool {
	return strings.Contains(filePath, "app/Http/Controllers/")
}

func (p *LaravelParser) isRelation(name string) bool {
	relations := []string{
		"hasOne", "hasMany", "belongsTo", "belongsToMany",
		"morphTo", "morphOne", "morphMany", "morphToMany",
	}
	for _, rel := range relations {
		if name == rel {
			return true
		}
	}
	return false
}

// GetSyntaxTree parses file and returns tree-sitter syntax tree for matchers.


// findExtends finds base class from class_declaration base_clause
func (p *LaravelParser) findExtends(root *sitter.Node, content []byte) string {
    var extends string
    var walk func(n *sitter.Node)
    walk = func(n *sitter.Node) {
        if n == nil { return }
        if n.Type() == "class_declaration" {
            for i := uint32(0); i < n.ChildCount(); i++ {
                ch := n.Child(int(i))
                if ch != nil && ch.Type() == "base_clause" {
                    for j := uint32(0); j < ch.ChildCount(); j++ {
                        q := ch.Child(int(j))
                        if q == nil { continue }
                        if q.Type() == "qualified_name" || q.Type() == "name" {
                            extends = string(content[q.StartByte():q.EndByte()])
                            return
                        }
                    }
                }
            }
        }
        for i := uint32(0); i < n.ChildCount(); i++ { walk(n.Child(int(i))) }
    }
    walk(root)
    return strings.TrimSpace(extends)
}

func (p *LaravelParser) extendsEloquentModel(extends string) bool {
    if extends == "" { return false }
    ext := strings.ReplaceAll(extends, " ", "")
    if strings.Contains(ext, "Illuminate\\Database\\Eloquent\\Model") {
        return true
    }
    if strings.HasSuffix(ext, "\\Model") || ext == "Model" {
        return true
    }
    return false
}

func (p *LaravelParser) isControllerByAST(namespace, className, extends string) bool {
    if className == "" { return false }
    if strings.HasSuffix(className, "Controller") { return true }
    if strings.Contains(extends, "Controller") { return true }
    if namespace != "" && strings.Contains(namespace, "\\Http\\Controllers") { return true }
    return false
}
func (p *LaravelParser) GetSyntaxTree(ctx context.Context, filePath string) (*SyntaxTree, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	tree, err := p.parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}
	
	// Wrap in SyntaxTree format that matchers expect
	rootNode := tree.RootNode()
	return &SyntaxTree{
		Root: &SyntaxNode{
			Type:       rootNode.Type(),
			StartByte:  int(rootNode.StartByte()),
			EndByte:    int(rootNode.EndByte()),
			StartPoint: Point{Row: int(rootNode.StartPoint().Row), Column: int(rootNode.StartPoint().Column)},
			EndPoint:   Point{Row: int(rootNode.EndPoint().Row), Column: int(rootNode.EndPoint().Column)},
		},
		Source: content,
	}, nil
}

// GetModelScopes returns discovered model scopes for the registry.
func (p *LaravelParser) GetModelScopes() map[string][]string {
	return p.ModelScopes
}

// GetControllers returns discovered controllers for the assembly phase.
func (p *LaravelParser) GetControllers() map[string][]string {
	return p.Controllers
}

// GetModels returns discovered models for the assembly phase.
func (p *LaravelParser) GetModels() map[string]ModelInfo {
	return p.Models
}
