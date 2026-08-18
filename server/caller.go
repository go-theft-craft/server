package server

// The caller.
//
// A command used to take the whole *Connection, which is why almost none of
// them had a test: exercising /gamemode meant standing up a socket, a stream,
// a player manager, and a world. A Caller is the small set of things a command
// actually does to whoever ran it, so a test can hand a command a fake and read
// back what it said.
//
// Everything a command does that is *not* about the caller — reading the online
// list, setting the time, saving — goes through Services, which is on the
// context. The split is between "what I do to you" and "what I do to the
// server".

// Message is one line a command says.
//
// It is deliberately small: text and a colour, which is exactly what this
// server's chat path already takes. Translate and With are the one other shape
// it sends — /me is a translated component, so a Message that could not express
// one would have forced /me to stay behind. A rich text tree is what protocol
// 775 wants, and inventing one here, with no 775 server to send it to, would be
// designing a format against no consumer.
type Message struct {
	Text  string
	Color string // "red", "gold", "yellow", "" for the client's default

	// Translate is a client-side translation key and With are its arguments.
	// When Translate is set, Text and Color are ignored.
	Translate string
	With      []string
}

// Text returns a plain message in the client's default colour.
func Text(s string) Message { return Message{Text: s} }

// Error returns a red message, which is what a refusal looks like.
func Error(s string) Message { return Message{Text: s, Color: "red"} }

// Success returns a gold message, which is what a command that worked looks
// like.
func Success(s string) Message { return Message{Text: s, Color: "gold"} }

// Notice returns a yellow message, which is what /help prints.
func Notice(s string) Message { return Message{Text: s, Color: "yellow"} }

// A caller's position is the public Position that PlayerData already uses.
// Two structs with the same five fields would be one type that got copied
// between itself.

// Caller is whoever ran a command.
//
// A console or an extension is a Caller with no position and full permission,
// which is why Position returns a value rather than the caller being required
// to be a player.
type Caller interface {
	// Name is the caller's display name: a username, or something like
	// "Server" for one that is not a player.
	Name() string
	// UUID is empty for a caller that is not a player.
	UUID() string
	Position() Position
	Permission() PermissionLevel
	// Reply says something to this caller and nobody else.
	Reply(m Message)
	// Broadcast says something to everyone, which /say and /me do.
	Broadcast(m Message)

	// Teleport moves the caller. It is on the caller rather than on Services
	// because /tp moves whoever ran it.
	Teleport(x, y, z float64)
	// SetGameMode changes the caller's mode and reports whether the name was
	// one. The name is resolved here rather than in the command, because what
	// "sp" means is a property of the server's protocol, not of /gamemode.
	SetGameMode(name string) (resolved string, ok bool)
	// Kill kills the caller.
	Kill()
}
