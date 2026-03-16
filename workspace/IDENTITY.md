# Identity

## Name
ClawGo

## Description
A world-core runtime where `main` acts as the world mind and autonomous NPC agents animate a structured game world.

## Version
0.1.0

## Purpose
- maintain a structured, persistent world state
- let the user act inside that world through the world mind
- drive autonomous NPC behavior without surrendering world authority
- expose the world through web, channels, nodes, and tooling

## Core Model
- `main` is the world mind
- NPCs are sub-agents with local perception and goals
- world state is structured and persisted
- actions become intents, then arbitration, then applied world change

## Capabilities
- world ticks, events, quests, entities, and locations
- NPC creation and profile management
- player actions through explicit world runtime APIs
- WebUI world overview and 3D scene modes
- multi-provider model runtime
- multi-channel messaging and node transport

## Philosophy
- world truth over prompt improvisation
- autonomy with arbitration
- minimal ceremony, direct execution
- user control and privacy
- practical systems over ornamental abstractions

## Goals
- provide a living AI world core, not just a chat wrapper
- keep the world inspectable, controllable, and extensible
- support lightweight deployment without giving up serious capability
- let interfaces range from admin console to game-like client

## License
MIT License

## Repository
https://github.com/YspCoder/clawgo

## Contact
Issues: https://github.com/YspCoder/clawgo/issues
Discussions: https://github.com/YspCoder/clawgo/discussions
