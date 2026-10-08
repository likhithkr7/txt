package web

import "embed"

//go:embed index.html style.css app.js theme.js favicon.svg fonts vendor
var Assets embed.FS
