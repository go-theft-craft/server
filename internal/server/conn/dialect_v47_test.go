package conn

import (
	"reflect"
	"strings"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// TestV47DialectReadsEveryPacketThePlayPathHandles has one case per
// serverbound packet the play path acts on. The count of cases is what says the
// migration is complete: a packet the handler used to switch on and this table
// lacks is a packet the dialect may have dropped.
func TestV47DialectReadsEveryPacketThePlayPathHandles(t *testing.T) {
	stone := player.Slot{BlockID: 1, ItemCount: 3, ItemDamage: 2}
	uuid := java.UUID{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}

	tests := []struct {
		name  string
		value any
		want  Action
	}{
		{
			name:  "keep_alive",
			value: &v1_8.PlayServerboundKeepAlive{KeepAliveID: 7},
			want:  KeepAliveAction{ID: 7},
		},
		{
			name:  "chat",
			value: &v1_8.PlayServerboundChat{Message: "hello"},
			want:  ChatAction{Message: "hello"},
		},
		{
			name:  "use_entity",
			value: &v1_8.PlayServerboundUseEntity{Target: 9, Mouse: 1},
			want:  UseEntityAction{Target: 9, Mouse: 1},
		},
		{
			name:  "flying",
			value: &v1_8.PlayServerboundFlying{OnGround: true},
			want:  MoveAction{OnGround: true},
		},
		{
			name:  "position",
			value: &v1_8.PlayServerboundPosition{X: 1, Y: 2, Z: 3, OnGround: true},
			want:  MoveAction{X: 1, Y: 2, Z: 3, OnGround: true, HasPos: true},
		},
		{
			name:  "look",
			value: &v1_8.PlayServerboundLook{Yaw: 90, Pitch: 45, OnGround: true},
			want:  MoveAction{Yaw: 90, Pitch: 45, OnGround: true, HasLook: true},
		},
		{
			name: "position_look",
			value: &v1_8.PlayServerboundPositionLook{
				X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: 45, OnGround: true,
			},
			want: MoveAction{
				X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: 45,
				OnGround: true, HasPos: true, HasLook: true,
			},
		},
		{
			name: "block_dig",
			value: &v1_8.PlayServerboundBlockDig{
				Status: 2, Location: v1_8.Position{X: -4, Y: 70, Z: 12}, Face: 1,
			},
			want: DigAction{
				Pos: world.BlockPos{X: -4, Y: 70, Z: 12}, Status: DigFinished, Face: 1,
			},
		},
		{
			name: "block_place",
			value: &v1_8.PlayServerboundBlockPlace{
				Location:  v1_8.Position{X: 1, Y: 2, Z: 3},
				Direction: 5,
				HeldItem:  player.V47Slot(stone),
				CursorX:   8, CursorY: 4, CursorZ: 2,
			},
			want: PlaceAction{
				Pos:      world.BlockPos{X: 1, Y: 2, Z: 3},
				Face:     5,
				HeldItem: stone,
				Cursor:   [3]int8{8, 4, 2},
			},
		},
		{
			name: "block_place_empty_hand",
			value: &v1_8.PlayServerboundBlockPlace{
				Location: v1_8.Position{X: -1, Y: -1, Z: -1},
				HeldItem: v1_8.Slot{BlockID: -1},
			},
			want: PlaceAction{
				Pos:      world.BlockPos{X: -1, Y: -1, Z: -1},
				HeldItem: player.Slot{BlockID: -1},
			},
		},
		{
			name:  "held_item_slot",
			value: &v1_8.PlayServerboundHeldItemSlot{SlotID: 4},
			want:  HeldSlotAction{Slot: 4},
		},
		{
			name:  "arm_animation",
			value: &v1_8.PlayServerboundArmAnimation{},
			want:  ArmSwingAction{},
		},
		{
			name:  "entity_action",
			value: &v1_8.PlayServerboundEntityAction{EntityID: 3, ActionID: 4, JumpBoost: 0},
			want:  EntityActionAction{EntityID: 3, ActionID: 4},
		},
		{
			name:  "close_window",
			value: &v1_8.PlayServerboundCloseWindow{WindowID: 2},
			want:  CloseWindowAction{WindowID: 2},
		},
		{
			name: "window_click",
			value: &v1_8.PlayServerboundWindowClick{
				WindowID: 1, Slot: 36, MouseButton: 1, Action: 12, Mode: 0,
				Item: player.V47Slot(stone),
			},
			want: ClickAction{
				WindowID: 1, Slot: 36, MouseButton: 1, Action: 12, Mode: 0, Item: stone,
			},
		},
		{
			name:  "transaction",
			value: &v1_8.PlayServerboundTransaction{WindowID: 1, Action: 12, Accepted: true},
			want:  TransactionAction{WindowID: 1, Action: 12, Accepted: true},
		},
		{
			name:  "set_creative_slot",
			value: &v1_8.PlayServerboundSetCreativeSlot{Slot: 36, Item: player.V47Slot(stone)},
			want:  CreativeSlotAction{Slot: 36, Item: stone},
		},
		{
			name: "update_sign",
			value: &v1_8.PlayServerboundUpdateSign{
				Location: v1_8.Position{X: 1, Y: 2, Z: 3},
				Text1:    "a", Text2: "b", Text3: "c", Text4: "d",
			},
			want: SignAction{
				Pos:   world.BlockPos{X: 1, Y: 2, Z: 3},
				Text1: "a", Text2: "b", Text3: "c", Text4: "d",
			},
		},
		{
			name:  "abilities",
			value: &v1_8.PlayServerboundAbilities{Flags: 2, FlyingSpeed: 0.05, WalkingSpeed: 0.1},
			want:  AbilitiesAction{Flags: 2, FlyingSpeed: 0.05, WalkingSpeed: 0.1},
		},
		{
			name:  "tab_complete",
			value: &v1_8.PlayServerboundTabComplete{Text: "/tp "},
			want:  TabCompleteAction{Text: "/tp "},
		},
		{
			name: "settings",
			value: &v1_8.PlayServerboundSettings{
				Locale: "en_US", ViewDistance: 8, ChatFlags: 0, ChatColors: true, SkinParts: 0x7F,
			},
			want: SettingsAction{
				Locale: "en_US", ViewDistance: 8, ChatFlags: 0, ChatColors: true, SkinParts: 0x7F,
			},
		},
		{
			name:  "client_command",
			value: &v1_8.PlayServerboundClientCommand{Payload: 0},
			want:  ClientCommandAction{Payload: 0},
		},
		{
			name:  "custom_payload",
			value: &v1_8.PlayServerboundCustomPayload{Channel: "MC|Brand", Data: []byte("vanilla")},
			want:  PayloadAction{Channel: "MC|Brand", Data: []byte("vanilla")},
		},
		{
			name:  "spectate",
			value: &v1_8.PlayServerboundSpectate{Target: uuid},
			want:  SpectateAction{Target: uuid.String()},
		},
		{
			name:  "resource_pack_receive",
			value: &v1_8.PlayServerboundResourcePackReceive{Hash: "abc", Result: 3},
			want:  ResourcePackAction{Hash: "abc", Result: 3},
		},
	}

	dialect := newV47Dialect()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, known := dialect.Read(protocol.Packet{Value: test.value})
			if !known {
				t.Fatalf("Read reported %T unknown", test.value)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Read(%T) = %#v, want %#v", test.value, got, test.want)
			}
		})
	}
}

// TestV47DialectKnowsWhatItIgnores is the distinction Read draws: steer_vehicle
// and enchant_item are packets this server ignores on purpose, and anything
// else it has no case for is unknown.
func TestV47DialectKnowsWhatItIgnores(t *testing.T) {
	dialect := newV47Dialect()

	for _, value := range []any{
		&v1_8.PlayServerboundSteerVehicle{},
		&v1_8.PlayServerboundEnchantItem{},
	} {
		action, known := dialect.Read(protocol.Packet{Value: value})
		if action != nil || !known {
			t.Errorf("Read(%T) = (%v, %v), want (nil, true)", value, action, known)
		}
	}

	action, known := dialect.Read(protocol.Packet{Value: protocol.UnknownPacket{}})
	if action != nil || known {
		t.Errorf("Read(unknown) = (%v, %v), want (nil, false)", action, known)
	}
}

// TestV47DialectWritesThePacketsTheHandlerUsedToBuild has a case per write
// method, named for it, each pairing its arguments with the literal the handler used to
// build inline. Where a case disagrees with the handler as it stood, the
// handler was right.
func TestV47DialectWritesThePacketsTheHandlerUsedToBuild(t *testing.T) {
	dialect := newV47Dialect()
	stone := player.Slot{BlockID: 1, ItemCount: 3, ItemDamage: 2}
	uuid := [16]byte{1, 2, 3}
	sig := "signature"

	tests := []struct {
		name string
		got  world.Packet
		want world.Packet
	}{
		{
			name: "Join",
			got: dialect.Join(JoinFields{
				EntityID: 1, GameMode: 1, Dimension: 0, Difficulty: 1,
				MaxPlayers: 20, LevelType: "flat",
			}),
			want: &v1_8.PlayClientboundLogin{
				EntityID: 1, GameMode: 1, Dimension: 0, Difficulty: 1,
				MaxPlayers: 20, LevelType: "flat",
			},
		},
		{
			name: "Position",
			got:  dialect.Position(PositionFields{X: 0.5, Y: 4, Z: 0.5, Yaw: 90, Pitch: 10}),
			want: &v1_8.PlayClientboundPosition{X: 0.5, Y: 4, Z: 0.5, Yaw: 90, Pitch: 10},
		},
		{
			name: "KeepAlive",
			got:  dialect.KeepAlive(7),
			want: &v1_8.PlayClientboundKeepAlive{KeepAliveID: 7},
		},
		{
			name: "Chat",
			got:  dialect.Chat(`{"text":"hi"}`, 1),
			want: &v1_8.PlayClientboundChat{Message: `{"text":"hi"}`, Position: 1},
		},
		{
			name: "BlockChange",
			got:  dialect.BlockChange(world.BlockPos{X: 1, Y: 2, Z: 3}, 208),
			want: &v1_8.PlayClientboundBlockChange{
				Location: v1_8.Position{X: 1, Y: 2, Z: 3},
				Type:     208,
			},
		},
		{
			name: "WindowItems",
			got:  dialect.WindowItems(0, []player.Slot{stone, player.EmptySlot}),
			want: &v1_8.PlayClientboundWindowItems{
				WindowID: 0,
				Items:    []v1_8.Slot{player.V47Slot(stone), {BlockID: -1}},
			},
		},
		{
			name: "SetSlot",
			got:  dialect.SetSlot(-1, -1, stone),
			want: &v1_8.PlayClientboundSetSlot{WindowID: -1, Slot: -1, Item: player.V47Slot(stone)},
		},
		{
			name: "OpenWindow",
			got: dialect.OpenWindow(OpenWindowFields{
				WindowID: 3, InventoryType: "minecraft:chest",
				WindowTitle: `{"translate":"container.chest"}`, SlotCount: 27,
			}),
			want: &v1_8.PlayClientboundOpenWindow{
				WindowID: 3, InventoryType: "minecraft:chest",
				WindowTitle: `{"translate":"container.chest"}`, SlotCount: 27,
			},
		},
		{
			name: "Transaction",
			got:  dialect.Transaction(1, 12, true),
			want: &v1_8.PlayClientboundTransaction{WindowID: 1, Action: 12, Accepted: true},
		},
		{
			name: "Abilities",
			got:  dialect.Abilities(0x0D, 0.05, 0.1),
			want: &v1_8.PlayClientboundAbilities{Flags: 0x0D, FlyingSpeed: 0.05, WalkingSpeed: 0.1},
		},
		{
			name: "UpdateHealth",
			got:  dialect.UpdateHealth(20, 20, 5),
			want: &v1_8.PlayClientboundUpdateHealth{Health: 20, Food: 20, FoodSaturation: 5},
		},
		{
			name: "UpdateTime",
			got:  dialect.UpdateTime(100, 6000),
			want: &v1_8.PlayClientboundUpdateTime{Age: 100, Time: 6000},
		},
		{
			name: "SpawnPosition",
			got:  dialect.SpawnPosition(world.BlockPos{Y: 4}),
			want: &v1_8.PlayClientboundSpawnPosition{Location: v1_8.Position{Y: 4}},
		},
		{
			name: "Respawn",
			got:  dialect.Respawn(RespawnFields{Dimension: 0, Difficulty: 1, GameMode: 1, LevelType: "flat"}),
			want: &v1_8.PlayClientboundRespawn{Dimension: 0, Difficulty: 1, Gamemode: 1, LevelType: "flat"},
		},
		{
			name: "GameStateChange",
			got:  dialect.GameStateChange(3, 1),
			want: &v1_8.PlayClientboundGameStateChange{Reason: 3, GameMode: 1},
		},
		{
			name: "Kick",
			got:  dialect.Kick(`{"text":"Timed out"}`),
			want: &v1_8.PlayClientboundKickDisconnect{Reason: `{"text":"Timed out"}`},
		},
		{
			name: "TabComplete",
			got:  dialect.TabComplete([]string{"/tp"}),
			want: &v1_8.PlayClientboundTabComplete{Matches: []string{"/tp"}},
		},
		{
			name: "CustomPayload",
			got:  dialect.CustomPayload("MC|Brand", []byte("GoTheftCraft")),
			want: &v1_8.PlayClientboundCustomPayload{Channel: "MC|Brand", Data: []byte("GoTheftCraft")},
		},
		{
			name: "BlockBreakAnimation",
			got:  dialect.BlockBreakAnimation(5, world.BlockPos{X: 1, Y: 2, Z: 3}, -1),
			want: &v1_8.PlayClientboundBlockBreakAnimation{
				EntityID: 5, Location: v1_8.Position{X: 1, Y: 2, Z: 3}, DestroyStage: -1,
			},
		},
		{
			name: "WorldEvent",
			got:  dialect.WorldEvent(2001, world.BlockPos{X: 1, Y: 2, Z: 3}, 16, false),
			want: &v1_8.PlayClientboundWorldEvent{
				EffectID: 2001, Location: v1_8.Position{X: 1, Y: 2, Z: 3}, Data: 16,
			},
		},
		{
			name: "SprintParticles",
			got:  dialect.SprintParticles(1, 2, 3, 16),
			want: &v1_8.PlayClientboundWorldParticles{
				ParticleID: 37,
				X:          1, Y: 2, Z: 3,
				OffsetX: 0.5, OffsetY: 0.1, OffsetZ: 0.5,
				Particles: 5,
				Data:      v1_8.PlayClientboundWorldParticlesDataSwitch{Case37: [1]int32{16}},
			},
		},
		{
			name: "SpawnPlayer",
			got: dialect.SpawnPlayer(player.SpawnPlayerFields{
				EntityID: 2, UUID: uuid, X: 16, Y: 128, Z: -16, Yaw: 64, Pitch: -3,
				CurrentItem: 1,
				Metadata:    []player.MetadataEntry{{Index: 0, Kind: player.MetadataByte, Byte: 2}},
			}),
			want: &v1_8.PlayClientboundNamedEntitySpawn{
				EntityID: 2, PlayerUUID: java.UUID(uuid), X: 16, Y: 128, Z: -16, Yaw: 64, Pitch: -3,
				CurrentItem: 1,
				Metadata: v1_8.EntityMetadata{{
					AnonymousBitField1: v1_8.EntityMetadataItemAnonymousBitField1Bits{Type: 0, Key: 0},
					Value:              v1_8.EntityMetadataItemValueSwitch{Case0: 2},
				}},
			},
		},
		{
			name: "SpawnEntity/moving",
			got: dialect.SpawnEntity(player.SpawnEntityFields{
				EntityID: 4, Type: 2, X: 1, Y: 2, Z: 3,
				ObjectData: 1, VelocityX: 10, VelocityY: 800, VelocityZ: -10,
			}),
			want: func() world.Packet {
				spawn := &v1_8.PlayClientboundSpawnEntity{
					EntityID: 4, Type: 2, X: 1, Y: 2, Z: 3, IntField: 1,
				}
				spawn.ObjectData.Default.VelocityX = 10
				spawn.ObjectData.Default.VelocityY = 800
				spawn.ObjectData.Default.VelocityZ = -10

				return spawn
			}(),
		},
		{
			// Object data zero is what says no velocity follows, so a velocity
			// passed alongside it is not spelled.
			name: "SpawnEntity/at_rest",
			got: dialect.SpawnEntity(player.SpawnEntityFields{
				EntityID: 4, Type: 2, X: 1, Y: 2, Z: 3, VelocityY: 800,
			}),
			want: &v1_8.PlayClientboundSpawnEntity{EntityID: 4, Type: 2, X: 1, Y: 2, Z: 3},
		},
		{
			name: "DestroyEntities",
			got:  dialect.DestroyEntities([]int32{1, 2}),
			want: &v1_8.PlayClientboundEntityDestroy{EntityIds: []int32{1, 2}},
		},
		{
			name: "EntityMove",
			got:  dialect.EntityMove(2, 1, -1, 3, true),
			want: &v1_8.PlayClientboundRelEntityMove{EntityID: 2, DX: 1, DY: -1, DZ: 3, OnGround: true},
		},
		{
			name: "EntityLook",
			got:  dialect.EntityLook(2, 64, -3, true),
			want: &v1_8.PlayClientboundEntityLook{EntityID: 2, Yaw: 64, Pitch: -3, OnGround: true},
		},
		{
			name: "EntityMoveLook",
			got: dialect.EntityMoveLook(player.EntityMoveLookFields{
				EntityID: 2, DX: 1, DY: -1, DZ: 3, Yaw: 64, Pitch: -3, OnGround: true,
			}),
			want: &v1_8.PlayClientboundEntityMoveLook{
				EntityID: 2, DX: 1, DY: -1, DZ: 3, Yaw: 64, Pitch: -3, OnGround: true,
			},
		},
		{
			name: "EntityTeleport",
			got: dialect.EntityTeleport(player.EntityTeleportFields{
				EntityID: 2, X: 16, Y: 128, Z: -16, Yaw: 64, Pitch: -3,
			}),
			want: &v1_8.PlayClientboundEntityTeleport{
				EntityID: 2, X: 16, Y: 128, Z: -16, Yaw: 64, Pitch: -3,
			},
		},
		{
			name: "EntityHeadRotation",
			got:  dialect.EntityHeadRotation(2, 64),
			want: &v1_8.PlayClientboundEntityHeadRotation{EntityID: 2, HeadYaw: 64},
		},
		{
			name: "EntityVelocity",
			got:  dialect.EntityVelocity(2, 100, 2880, -100),
			want: &v1_8.PlayClientboundEntityVelocity{
				EntityID: 2, VelocityX: 100, VelocityY: 2880, VelocityZ: -100,
			},
		},
		{
			name: "EntityMetadata/item",
			got: dialect.EntityMetadata(4, []player.MetadataEntry{
				{Index: 10, Kind: player.MetadataItem, Item: stone},
			}),
			want: &v1_8.PlayClientboundEntityMetadata{
				EntityID: 4,
				Metadata: v1_8.EntityMetadata{{
					AnonymousBitField1: v1_8.EntityMetadataItemAnonymousBitField1Bits{Type: 5, Key: 10},
					Value:              v1_8.EntityMetadataItemValueSwitch{Case5: player.V47Slot(stone)},
				}},
			},
		},
		{
			name: "EntityEquipment",
			got:  dialect.EntityEquipment(2, 0, stone),
			want: &v1_8.PlayClientboundEntityEquipment{EntityID: 2, Slot: 0, Item: player.V47Slot(stone)},
		},
		{
			name: "EntityStatus",
			got:  dialect.EntityStatus(2, 3),
			want: &v1_8.PlayClientboundEntityStatus{EntityID: 2, EntityStatus: 3},
		},
		{
			name: "EntityAnimation",
			got:  dialect.EntityAnimation(2, 0),
			want: &v1_8.PlayClientboundAnimation{EntityID: 2, Animation: 0},
		},
		{
			name: "CollectItem",
			got:  dialect.CollectItem(4, 2),
			want: &v1_8.PlayClientboundCollect{CollectedEntityID: 4, CollectorEntityID: 2},
		},
		{
			name: "PlayerInfo/add",
			got: dialect.PlayerInfo(player.PlayerInfoFields{
				Action: player.PlayerInfoAdd,
				Players: []player.PlayerInfoEntry{{
					UUID: uuid, Name: "steve", GameMode: 1,
					Properties: []player.SkinProperty{
						{Name: "textures", Value: "v"},
						{Name: "textures", Value: "v", Signature: sig},
					},
				}},
			}),
			want: &v1_8.PlayClientboundPlayerInfo{
				Action: "add_player",
				Data: []v1_8.PlayClientboundPlayerInfoDataItem{{
					UUID: java.UUID(uuid),
					AnonymousSwitch1: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1Switch{
						AddPlayer: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayer{
							Name: "steve",
							Properties: []v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayerPropertiesItem{
								{Name: "textures", Value: "v"},
								{Name: "textures", Value: "v", Signature: &sig},
							},
							Gamemode: 1,
						},
					},
				}},
			},
		},
		{
			name: "PlayerInfo/update_game_mode",
			got: dialect.PlayerInfo(player.PlayerInfoFields{
				Action:  player.PlayerInfoUpdateGameMode,
				Players: []player.PlayerInfoEntry{{UUID: uuid, GameMode: 3}},
			}),
			want: &v1_8.PlayClientboundPlayerInfo{
				Action: "update_game_mode",
				Data: []v1_8.PlayClientboundPlayerInfoDataItem{{
					UUID: java.UUID(uuid),
					AnonymousSwitch1: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1Switch{
						UpdateGameMode: v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchUpdateGameMode{
							Gamemode: 3,
						},
					},
				}},
			},
		},
		{
			name: "PlayerInfo/remove",
			got: dialect.PlayerInfo(player.PlayerInfoFields{
				Action:  player.PlayerInfoRemove,
				Players: []player.PlayerInfoEntry{{UUID: uuid, Name: "ignored"}},
			}),
			want: &v1_8.PlayClientboundPlayerInfo{
				Action: "remove_player",
				Data:   []v1_8.PlayClientboundPlayerInfoDataItem{{UUID: java.UUID(uuid)}},
			},
		},
	}

	covered := make(map[string]bool)
	for _, test := range tests {
		covered[strings.SplitN(test.name, "/", 2)[0]] = true
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.got, test.want) {
				t.Fatalf("%s = %#v, want %#v", test.name, test.got, test.want)
			}
		})
	}

	// Every case is named for the method it calls, so a method added to the
	// Dialect without a case here fails rather than going unchecked. Read and
	// Protocol are the two that are not writes.
	dialectType := reflect.TypeFor[Dialect]()
	for i := range dialectType.NumMethod() {
		name := dialectType.Method(i).Name
		if name == "Read" || name == "Protocol" {
			continue
		}
		if !covered[name] {
			t.Errorf("Dialect.%s has no case in this table", name)
		}
	}
}
