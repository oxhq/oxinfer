package parser

import (
	"context"
	"fmt"
	"os"
	"strings"
	
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

// ParseFile parses a PHP file and extracts what matters.
func (p *LaravelParser) ParseFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	
	tree, err := p.parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return fmt.Errorf("failed to parse: %w", err)
	}
	defer tree.Close()
	
	root := tree.RootNode()
	
	// Route to appropriate processor
	if p.isModel(filePath) {
		p.extractModel(root, content, filePath)
	} else if p.isController(filePath) {
		p.extractController(root, content, filePath)
	}
	
	return nil
}

func (p *LaravelParser) extractModel(root *sitter.Node, content []byte, filePath string) {
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
func (p *LaravelParser) extractController(root *sitter.Node, content []byte, filePath string) {
	className := p.findClassName(root, content)
	if className == "" {
		return
	}
	
	namespace := p.findNamespace(root, content)
	fqcn := className
	if namespace != "" {
		fqcn = namespace + "\\" + className
	}
	
	// Find public methods (actions)
	var methods []string
	
	p.walkNode(root, content, func(node *sitter.Node) bool {
		if node.Type() == "method_declaration" {
			// Check if public
			for i := uint32(0); i < node.ChildCount(); i++ {
				child := node.Child(int(i))
				if child.Type() == "visibility_modifier" {
					visibility := string(content[child.StartByte():child.EndByte()])
					if visibility == "public" {
						methodName := p.getMethodName(node, content)
						methods = append(methods, methodName)
						break
					}
				}
			}
		}
		return true
	})
	
	if len(methods) > 0 {
		p.Controllers[fqcn] = methods
	}
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
	return strings.Contains(filePath, "app/Models/") ||
		(strings.Contains(filePath, "app/") && !strings.Contains(filePath, "app/Http/"))
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
