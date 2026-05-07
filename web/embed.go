package webassets

import "embed"

//go:embed index.html css/* js/*
var Files embed.FS
