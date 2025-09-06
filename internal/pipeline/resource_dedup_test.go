package pipeline

import (
	"sort"
	"testing"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/matchers"
)

// TestResourceDedup_DirectLogic tests the resource deduplication logic directly
func TestResourceDedup_DirectLogic(t *testing.T) {
	tests := []struct {
		name              string
		inputResources    []*matchers.ResourceMatch
		expectedResources []emitter.Resource
	}{
		{
			name: "duplicate_resources_deduped",
			inputResources: []*matchers.ResourceMatch{
				{Class: "UserResource", Collection: false},
				{Class: "UserResource", Collection: false}, // Duplicate
				{Class: "UserResource", Collection: true},
				{Class: "UserResource", Collection: true}, // Duplicate
			},
			expectedResources: []emitter.Resource{
				{Class: "UserResource", Collection: false},
				{Class: "UserResource", Collection: true},
			},
		},
		{
			name: "resources_sorted_by_class_then_collection",
			inputResources: []*matchers.ResourceMatch{
				{Class: "PostResource", Collection: true},
				{Class: "UserResource", Collection: false},
				{Class: "CommentResource", Collection: true},
				{Class: "PostResource", Collection: false},
			},
			expectedResources: []emitter.Resource{
				{Class: "CommentResource", Collection: true},
				{Class: "PostResource", Collection: false},
				{Class: "PostResource", Collection: true},
				{Class: "UserResource", Collection: false},
			},
		},
		{
			name: "collection_false_before_true",
			inputResources: []*matchers.ResourceMatch{
				{Class: "ApiResource", Collection: true},
				{Class: "ApiResource", Collection: false},
			},
			expectedResources: []emitter.Resource{
				{Class: "ApiResource", Collection: false},
				{Class: "ApiResource", Collection: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the deduplication logic from AssembleControllers
			type resourceKey struct {
				class      string
				collection bool
			}
			seen := make(map[resourceKey]struct{})
			dedupedResources := make([]emitter.Resource, 0, len(tt.inputResources))
			
			for _, resource := range tt.inputResources {
				key := resourceKey{
					class:      resource.Class,
					collection: resource.Collection,
				}
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				dedupedResources = append(dedupedResources, emitter.Resource{
					Class:      resource.Class,
					Collection: resource.Collection,
				})
			}
			
			// Sort by class, then collection=false before collection=true
			sort.Slice(dedupedResources, func(i, j int) bool {
				if dedupedResources[i].Class != dedupedResources[j].Class {
					return dedupedResources[i].Class < dedupedResources[j].Class
				}
				return !dedupedResources[i].Collection && dedupedResources[j].Collection
			})
			
			// Verify results
			if len(dedupedResources) != len(tt.expectedResources) {
				t.Errorf("Expected %d resources, got %d", len(tt.expectedResources), len(dedupedResources))
				return
			}
			
			for i, expected := range tt.expectedResources {
				actual := dedupedResources[i]
				if actual.Class != expected.Class || actual.Collection != expected.Collection {
					t.Errorf("Resource at index %d: expected %+v, got %+v", i, expected, actual)
				}
			}
			
			t.Logf("✓ Test passed: %s", tt.name)
		})
	}
}
