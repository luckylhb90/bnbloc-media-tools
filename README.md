# bnbloc-media-tools

**Images and transcripts for [bnbloc](https://github.com/luckylhb90/bnbloc) room agents.**

An installable MCP tool component with two tools, `generate_image` (draw or
edit an image; the result is published into the room's Docs, never passed
through the model's context) and `transcribe_audio` (an attachment's
transcript, fenced as data). It calls the deployment's media providers.

Nothing to configure: the room is the credential (`mcp.credentials: "room"`).
Each call reaches the gateway through the bridge's room relay, as the person
the agent is answering.

## Install into bnbloc

Download the binaries and `package.json` from the
[releases](https://github.com/luckylhb90/bnbloc-media-tools/releases) (or build them,
below), then as a bnbloc super-admin:

```sh
bnbloc cores publish-mcp --gateway https://<gateway> --package package.json \
  --binary darwin-arm64=dist/bnbloc-media-tools-darwin-arm64 --binary darwin-amd64=dist/bnbloc-media-tools-darwin-amd64 \
  --binary linux-amd64=dist/bnbloc-media-tools-linux-amd64 --binary windows-amd64=dist/bnbloc-media-tools-windows-amd64.exe \
  --rollout optin
```

Members who opt in (or everyone, with `--rollout auto`) get it in their
desktop app.

## Build and test

```sh
go test ./...
go build -o bnbloc-media-tools .
```

It builds against bnbloc's public packages (`component`, `core`,
`core/pkgmeta`), pinned in `go.mod`. A tag `vX.Y.Z` builds every platform
binary in CI and attaches them to the release; the tag's version is the
`version` in `package.json`.

## License

[Apache License 2.0](LICENSE).
