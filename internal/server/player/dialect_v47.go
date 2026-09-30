package player

import (
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/pkg/world"
)

// V47Packets spells Packets in protocol 47.
//
// It is one of the two files in this package allowed to name a generated
// version package, and the connection's protocol 47 dialect embeds it rather
// than repeating it. Every method is the packet literal that used to be written
// inline where the packet was sent, moved rather than rewritten, and the
// byte-parity fixtures are what say nothing moved with it.
type V47Packets struct{}

var _ Packets = V47Packets{}

func (V47Packets) SpawnPlayer(f SpawnPlayerFields) world.Packet {
	return &v1_8.PlayClientboundNamedEntitySpawn{
		EntityID:    f.EntityID,
		PlayerUUID:  java.UUID(f.UUID),
		X:           f.X,
		Y:           f.Y,
		Z:           f.Z,
		Yaw:         f.Yaw,
		Pitch:       f.Pitch,
		CurrentItem: f.CurrentItem,
		Metadata:    v47Metadata(f.Metadata),
	}
}

// SpawnEntity leaves the velocity off unless ObjectData says it follows, which
// is what the generated switch encodes: the late-join spawn of an item at rest
// carries none.
func (V47Packets) SpawnEntity(f SpawnEntityFields) world.Packet {
	spawn := &v1_8.PlayClientboundSpawnEntity{
		EntityID: f.EntityID,
		Type:     f.Type,
		X:        f.X,
		Y:        f.Y,
		Z:        f.Z,
		Pitch:    f.Pitch,
		Yaw:      f.Yaw,
		IntField: f.ObjectData,
	}
	if f.ObjectData != 0 {
		spawn.ObjectData.Default.VelocityX = f.VelocityX
		spawn.ObjectData.Default.VelocityY = f.VelocityY
		spawn.ObjectData.Default.VelocityZ = f.VelocityZ
	}

	return spawn
}

func (V47Packets) DestroyEntities(ids []int32) world.Packet {
	return &v1_8.PlayClientboundEntityDestroy{EntityIds: ids}
}

func (V47Packets) EntityMove(id int32, dx, dy, dz int8, onGround bool) world.Packet {
	return &v1_8.PlayClientboundRelEntityMove{
		EntityID: id,
		DX:       dx,
		DY:       dy,
		DZ:       dz,
		OnGround: onGround,
	}
}

func (V47Packets) EntityLook(id int32, yaw, pitch int8, onGround bool) world.Packet {
	return &v1_8.PlayClientboundEntityLook{
		EntityID: id,
		Yaw:      yaw,
		Pitch:    pitch,
		OnGround: onGround,
	}
}

func (V47Packets) EntityMoveLook(f EntityMoveLookFields) world.Packet {
	return &v1_8.PlayClientboundEntityMoveLook{
		EntityID: f.EntityID,
		DX:       f.DX,
		DY:       f.DY,
		DZ:       f.DZ,
		Yaw:      f.Yaw,
		Pitch:    f.Pitch,
		OnGround: f.OnGround,
	}
}

func (V47Packets) EntityTeleport(f EntityTeleportFields) world.Packet {
	return &v1_8.PlayClientboundEntityTeleport{
		EntityID: f.EntityID,
		X:        f.X,
		Y:        f.Y,
		Z:        f.Z,
		Yaw:      f.Yaw,
		Pitch:    f.Pitch,
		OnGround: f.OnGround,
	}
}

func (V47Packets) EntityHeadRotation(id int32, yaw int8) world.Packet {
	return &v1_8.PlayClientboundEntityHeadRotation{EntityID: id, HeadYaw: yaw}
}

func (V47Packets) EntityVelocity(id int32, dx, dy, dz int16) world.Packet {
	return &v1_8.PlayClientboundEntityVelocity{
		EntityID:  id,
		VelocityX: dx,
		VelocityY: dy,
		VelocityZ: dz,
	}
}

func (V47Packets) EntityMetadata(id int32, entries []MetadataEntry) world.Packet {
	return &v1_8.PlayClientboundEntityMetadata{
		EntityID: id,
		Metadata: v47Metadata(entries),
	}
}

func (V47Packets) EntityEquipment(id int32, slot int16, item Slot) world.Packet {
	return &v1_8.PlayClientboundEntityEquipment{
		EntityID: id,
		Slot:     slot,
		Item:     V47Slot(item),
	}
}

func (V47Packets) EntityStatus(id int32, status int8) world.Packet {
	return &v1_8.PlayClientboundEntityStatus{EntityID: id, EntityStatus: status}
}

func (V47Packets) EntityAnimation(id int32, animation uint8) world.Packet {
	return &v1_8.PlayClientboundAnimation{EntityID: id, Animation: animation}
}

func (V47Packets) CollectItem(collected, collector int32) world.Packet {
	return &v1_8.PlayClientboundCollect{
		CollectedEntityID: collected,
		CollectorEntityID: collector,
	}
}

func (V47Packets) PlayerInfo(f PlayerInfoFields) world.Packet {
	data := make([]v1_8.PlayClientboundPlayerInfoDataItem, len(f.Players))
	for i, entry := range f.Players {
		data[i].UUID = java.UUID(entry.UUID)
		switch f.Action {
		case PlayerInfoAdd:
			data[i].AnonymousSwitch1.AddPlayer = v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayer{
				Name:       entry.Name,
				Properties: v47Properties(entry.Properties),
				Gamemode:   entry.GameMode,
				Ping:       entry.Ping,
			}
		case PlayerInfoUpdateGameMode:
			data[i].AnonymousSwitch1.UpdateGameMode = v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchUpdateGameMode{
				Gamemode: entry.GameMode,
			}
		case PlayerInfoRemove:
		}
	}

	return &v1_8.PlayClientboundPlayerInfo{
		Action: v47PlayerInfoAction(f.Action),
		Data:   data,
	}
}

func v47PlayerInfoAction(action PlayerInfoAction) string {
	switch action {
	case PlayerInfoAdd:
		return "add_player"
	case PlayerInfoUpdateGameMode:
		return "update_game_mode"
	default:
		return "remove_player"
	}
}

func v47Properties(props []SkinProperty) []v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayerPropertiesItem {
	items := make([]v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayerPropertiesItem, 0, len(props))
	for _, prop := range props {
		item := v1_8.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayerPropertiesItem{
			Name:  prop.Name,
			Value: prop.Value,
		}
		if prop.Signature != "" {
			sig := prop.Signature
			item.Signature = &sig
		}
		items = append(items, item)
	}

	return items
}

// Metadata type IDs for MC 1.8 entity metadata format.
const (
	metaTypeByte = 0
	metaTypeSlot = 5
)

// v47Metadata spells a metadata list in protocol 47. The generated codec packs
// each header as (Type<<5)|Key, matching the 1.8 wire header
// (index & 0x1F) | (typeID << 5), and appends the 0x7F terminator on encode, so
// entries carry no terminator of their own.
func v47Metadata(entries []MetadataEntry) v1_8.EntityMetadata {
	out := make(v1_8.EntityMetadata, len(entries))
	for i, entry := range entries {
		switch entry.Kind {
		case MetadataItem:
			out[i] = v1_8.EntityMetadataItem{
				AnonymousBitField1: v1_8.EntityMetadataItemAnonymousBitField1Bits{
					Type: metaTypeSlot,
					Key:  entry.Index,
				},
				Value: v1_8.EntityMetadataItemValueSwitch{Case5: V47Slot(entry.Item)},
			}
		default:
			out[i] = v1_8.EntityMetadataItem{
				AnonymousBitField1: v1_8.EntityMetadataItemAnonymousBitField1Bits{
					Type: metaTypeByte,
					Key:  entry.Index,
				},
				Value: v1_8.EntityMetadataItemValueSwitch{Case0: entry.Byte},
			}
		}
	}

	return out
}

// V47Slot converts a Slot to the generated protocol 47 Slot value.
//
// It mirrors WriteSlot's wire shape: for an empty slot (BlockID -1) the
// generated Slot.Encode writes only the block ID, and for a present item it
// writes count, damage, and -- with NBTData nil -- the single-byte no-NBT
// sentinel WriteSlot emits by hand.
func V47Slot(s Slot) v1_8.Slot {
	slot := v1_8.Slot{BlockID: s.BlockID}
	if s.BlockID != -1 {
		slot.AnonymousSwitch1.Default.ItemCount = s.ItemCount
		slot.AnonymousSwitch1.Default.ItemDamage = s.ItemDamage
	}

	return slot
}

// SlotFromV47 converts a decoded protocol 47 Slot into a Slot. An empty slot
// (BlockID -1) carries no count or damage; the generated model leaves those
// switch fields zero, which this preserves.
func SlotFromV47(s v1_8.Slot) Slot {
	if s.BlockID == -1 {
		return Slot{BlockID: -1}
	}

	return Slot{
		BlockID:    s.BlockID,
		ItemCount:  s.AnonymousSwitch1.Default.ItemCount,
		ItemDamage: s.AnonymousSwitch1.Default.ItemDamage,
	}
}
