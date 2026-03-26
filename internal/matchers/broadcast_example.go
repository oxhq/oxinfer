package matchers

import (
	"context"
	"fmt"
	"log"

	"github.com/oxhq/oxinfer/internal/parser"
	"github.com/smacker/go-tree-sitter/php"
)

// ExampleBroadcastMatcher demonstrates how to use the BroadcastMatcher.
func ExampleBroadcastMatcher() {
	// Create a new broadcast matcher with PHP language support
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewBroadcastMatcher(language, config)
	if err != nil {
		log.Fatalf("Failed to create broadcast matcher: %v", err)
	}
	defer matcher.Close()

	// Example PHP code with broadcast channel definitions
	phpCode := `<?php

use Illuminate\Broadcasting\Broadcast;

// Public channel accessible to all users
Broadcast::channel('notifications', function () {
    return true;
});

// Private channel with user authorization
Broadcast::private('user.{id}', function ($user, $id) {
    return (int) $user->id === (int) $id;
});

// Presence channel that tracks connected users
Broadcast::presence('chat.{room}', function ($user, $room) {
    if ($user->canAccessRoom($room)) {
        return [
            'id' => $user->id,
            'name' => $user->name,
            'avatar' => $user->avatar_url,
        ];
    }
    return false;
});

// Channel with multiple parameters
Broadcast::private('orders.{orderId}.comments.{commentId}', function ($user, $orderId, $commentId) {
    $order = Order::find($orderId);
    $comment = Comment::find($commentId);
    
    return $user->id === $order->user_id || $user->id === $comment->user_id;
});

// Routes/channels.php style function calls
channel('public.announcements', function () {
    return true;
});

private('admin.notifications', function ($user) {
    return $user->isAdmin();
});
`

	// Create syntax tree (in a real implementation, this would come from the parser)
	tree := &parser.SyntaxTree{
		Source:   []byte(phpCode),
		Root:     &parser.SyntaxNode{Type: "program", Text: phpCode},
		Language: "php",
	}

	// Find all broadcast channel patterns
	ctx := context.Background()
	matches, err := matcher.MatchBroadcast(ctx, tree, "routes/channels.php")
	if err != nil {
		log.Printf("Error matching broadcast patterns: %v", err)
		// In a real implementation, this might be acceptable and we'd continue
		// For the example, we'll show what the expected output would look like
		showExpectedResults()
		return
	}

	// Display the results
	fmt.Printf("Found %d broadcast channel patterns:\n\n", len(matches))
	for i, match := range matches {
		fmt.Printf("Match %d:\n", i+1)
		fmt.Printf("  Channel: %s\n", match.Channel)
		fmt.Printf("  Visibility: %s\n", match.Visibility)
		fmt.Printf("  Method: %s\n", match.Method)
		fmt.Printf("  Parameters: %v\n", match.Params)
		fmt.Printf("  Pattern: %s\n", match.Pattern)
		fmt.Printf("  File: %s\n", match.File)
		fmt.Printf("  Payload Literal: %t\n", match.PayloadLiteral)
		fmt.Println()
	}
}

// showExpectedResults demonstrates the expected output structure.
func showExpectedResults() {
	fmt.Println("Expected broadcast channel patterns:")
	fmt.Println()

	expectedMatches := []*BroadcastMatch{
		{
			Channel:        "notifications",
			Visibility:     "public",
			Method:         "channel",
			Params:         []string{},
			Pattern:        "broadcast_channel_public",
			File:           "routes/channels.php",
			PayloadLiteral: true,
		},
		{
			Channel:        "user.{id}",
			Visibility:     "private",
			Method:         "private",
			Params:         []string{"id"},
			Pattern:        "broadcast_private_channel",
			File:           "routes/channels.php",
			PayloadLiteral: false,
		},
		{
			Channel:        "chat.{room}",
			Visibility:     "presence",
			Method:         "presence",
			Params:         []string{"room"},
			Pattern:        "broadcast_presence_channel",
			File:           "routes/channels.php",
			PayloadLiteral: false,
		},
		{
			Channel:        "orders.{orderId}.comments.{commentId}",
			Visibility:     "private",
			Method:         "private",
			Params:         []string{"commentId", "orderId"}, // Sorted alphabetically
			Pattern:        "broadcast_private_channel",
			File:           "routes/channels.php",
			PayloadLiteral: false,
		},
		{
			Channel:        "public.announcements",
			Visibility:     "public",
			Method:         "channel",
			Params:         []string{},
			Pattern:        "broadcast_in_routes_file",
			File:           "routes/channels.php",
			PayloadLiteral: true,
		},
		{
			Channel:        "admin.notifications",
			Visibility:     "private",
			Method:         "private",
			Params:         []string{},
			Pattern:        "broadcast_in_routes_file",
			File:           "routes/channels.php",
			PayloadLiteral: false,
		},
	}

	for i, match := range expectedMatches {
		fmt.Printf("Expected Match %d:\n", i+1)
		fmt.Printf("  Channel: %s\n", match.Channel)
		fmt.Printf("  Visibility: %s\n", match.Visibility)
		fmt.Printf("  Method: %s\n", match.Method)
		fmt.Printf("  Parameters: %v\n", match.Params)
		fmt.Printf("  Pattern: %s\n", match.Pattern)
		fmt.Printf("  File: %s\n", match.File)
		fmt.Printf("  Payload Literal: %t\n", match.PayloadLiteral)
		fmt.Println()
	}
}

// ExampleBroadcastValidation demonstrates broadcast channel validation.
func ExampleBroadcastValidation() {
	fmt.Println("Broadcast Channel Validation Examples:")
	fmt.Println()

	validations := []struct {
		method      string
		channel     string
		hasCallback bool
		description string
	}{
		{"channel", "notifications", true, "Public channel with callback"},
		{"channel", "announcements", false, "Public channel without callback"},
		{"private", "user.{id}", true, "Private channel with callback"},
		{"private", "user.{id}", false, "Private channel without callback (invalid)"},
		{"presence", "chat.{room}", true, "Presence channel with callback"},
		{"presence", "chat.{room}", false, "Presence channel without callback (invalid)"},
		{"invalid", "test", true, "Invalid method name"},
		{"channel", "", true, "Empty channel name (invalid)"},
	}

	for _, v := range validations {
		isValid := ValidateBroadcastChannelCall(v.method, v.channel, v.hasCallback)
		status := "✓ Valid"
		if !isValid {
			status = "✗ Invalid"
		}
		fmt.Printf("%s - %s: %s(%q, callback=%t)\n",
			status, v.description, v.method, v.channel, v.hasCallback)
	}
}

// ExampleBroadcastPatterns shows supported Laravel broadcast patterns.
func ExampleBroadcastPatterns() {
	fmt.Println("Supported Laravel Broadcast Patterns:")
	fmt.Println()

	patterns := GetSupportedBroadcastPatterns()
	for i, pattern := range patterns {
		fmt.Printf("%d. %s\n", i+1, pattern)
	}

	fmt.Println("\nBroadcast Method Conventions:")
	fmt.Println()

	conventions := GetBroadcastMethodConventions()
	for method, description := range conventions {
		fmt.Printf("%-10s: %s\n", method, description)
	}
}
