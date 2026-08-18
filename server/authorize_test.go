package server_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-theft-craft/server/server"
)

// Permissions.
//
// The default grants everything, which will read as an oversight to anybody
// who finds it without the reason. The reason: this server has no operator
// list, and a framework milestone that silently introduced one would lock
// people out of their own worlds on upgrade. The test name says it, the
// option's doc comment says it, and the milestone record says it a third time.

func TestTheDefaultAuthorizerGrantsEverything(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	caller := newFakeCaller("Alice")
	caller.permission = server.PermissionEveryone
	svc := newFakeServices(server.BuiltinCommands())

	srv.Dispatch(server.WithServices(context.Background(), svc), caller, "/seed")

	if strings.Contains(caller.lastReply(), "Unknown") {
		t.Errorf("a server with no authorizer refused /seed: %q", caller.lastReply())
	}
	if !strings.Contains(caller.lastReply(), "1234") {
		t.Errorf("/seed replied %q", caller.lastReply())
	}
}

// byLevel refuses anything above the caller's own level.
func byLevel() server.Authorizer {
	return func(caller server.Caller, cmd *server.Command) bool {
		return caller.Permission() >= cmd.Permission
	}
}

func TestAnUnauthorizedCommandIsRefused(t *testing.T) {
	restricted, err := server.NewSet(server.Command{
		Name:       "shutdown",
		Permission: server.PermissionOwner,
		Signature:  server.Signature{Overloads: []server.Overload{{}}},
		Run: func(_ context.Context, c server.Caller, _ server.Args) error {
			c.Reply(server.Success("shutting down"))

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}

	srv, err := server.New(server.WithCommands(restricted), server.WithAuthorizer(byLevel()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	caller := newFakeCaller("Alice")
	caller.permission = server.PermissionEveryone
	srv.Dispatch(context.Background(), caller, "/shutdown")

	if strings.Contains(caller.lastReply(), "shutting down") {
		t.Fatal("a command above the caller's level ran")
	}
	// Refused as unknown rather than as forbidden, deliberately: "you may not
	// run /shutdown" tells somebody that /shutdown exists, which is the thing
	// an authorizer was installed to stop.
	if !strings.Contains(caller.lastReply(), "Unknown command") {
		t.Errorf("a refusal replied %q, want it indistinguishable from an unknown command", caller.lastReply())
	}

	// The same command, with the level for it.
	allowed := newFakeCaller("Root")
	allowed.permission = server.PermissionOwner
	srv.Dispatch(context.Background(), allowed, "/shutdown")

	if !strings.Contains(allowed.lastReply(), "shutting down") {
		t.Errorf("an authorized caller was refused: %q", allowed.lastReply())
	}
}

func TestAnUnauthorizedCommandIsNotSuggested(t *testing.T) {
	// The information-leak case. Completing /ban for somebody who cannot run
	// it tells them the server has one, which is exactly what an authorizer is
	// there to withhold.
	restricted, err := server.NewSet(
		server.Command{
			Name: "ban", Permission: server.PermissionAdmin,
			Signature: server.Signature{Overloads: []server.Overload{{Params: []server.Param{
				{Name: "player", Type: server.ParamPlayer},
			}}}},
			Run: func(context.Context, server.Caller, server.Args) error { return nil },
		},
		server.Command{
			Name: "balance", Permission: server.PermissionEveryone,
			Signature: server.Signature{Overloads: []server.Overload{{}}},
			Run:       func(context.Context, server.Caller, server.Args) error { return nil },
		},
	)
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}

	srv, err := server.New(server.WithCommands(restricted), server.WithAuthorizer(byLevel()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc := newFakeServices(restricted)
	ctx := server.WithServices(context.Background(), svc)

	player := newFakeCaller("Alice")
	player.permission = server.PermissionEveryone

	got := srv.Complete(ctx, player, "/ba")
	for _, match := range got {
		if match == "/ban" {
			t.Error("/ban was suggested to a caller who cannot run it")
		}
	}
	if len(got) != 1 || got[0] != "/balance" {
		t.Errorf("completing /ba gave %v, want only /balance", got)
	}

	// Its arguments are not suggested either, which is the same leak one word
	// further in.
	if args := srv.Complete(ctx, player, "/ban "); len(args) != 0 {
		t.Errorf("a forbidden command suggested arguments: %v", args)
	}

	admin := newFakeCaller("Root")
	admin.permission = server.PermissionAdmin
	if got := srv.Complete(ctx, admin, "/ba"); len(got) != 2 {
		t.Errorf("an admin was offered %v, want both commands", got)
	}
}

func TestAllowAllIsTheDefaultSpelledOut(t *testing.T) {
	srv, err := server.New(server.WithAuthorizer(server.AllowAll()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	caller := newFakeCaller("Alice")
	caller.permission = server.PermissionEveryone
	srv.Dispatch(server.WithServices(context.Background(), newFakeServices(server.BuiltinCommands())),
		caller, "/seed")

	if strings.Contains(caller.lastReply(), "Unknown") {
		t.Errorf("AllowAll refused a command: %q", caller.lastReply())
	}
}

func TestANilAuthorizerIsRejectedRatherThanIgnored(t *testing.T) {
	if _, err := server.New(server.WithAuthorizer(nil)); err == nil {
		t.Error("a nil authorizer was accepted; omitting the option is how you allow everything")
	}
}
