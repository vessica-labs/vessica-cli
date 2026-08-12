package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

// Break caught: adapters get separate persistence paths for the same shared
// conversation and cannot reliably observe the messages created by the other.
func TestCloudAgentServiceUsesSharedConversationState(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err = db.EnsureWorkspace(ctx, root, "hosted"); err != nil {
		t.Fatal(err)
	}
	service := New(db, root, config.Defaults())
	conversation, err := service.StartConversation(ctx, state.ConversationInput{ActorID: "user_1", Title: "MCP and web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddConversationMessage(ctx, conversation.ID, state.ConversationMessageInput{Role: "user", ContentJSON: `{"text":"hello"}`}); err != nil {
		t.Fatal(err)
	}
	messages, err := service.ConversationMessages(ctx, conversation.ID, 0)
	if err != nil || len(messages) != 1 || messages[0].ContentJSON != `{"text":"hello"}` {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
}
