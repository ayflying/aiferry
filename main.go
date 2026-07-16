package main

import (
	_ "github.com/yunloli/aiferry/internal/packed"

	"github.com/gogf/gf/v2/os/gctx"

	"github.com/yunloli/aiferry/internal/cmd"
)

func main() {
	cmd.Main.Run(gctx.GetInitCtx())
}
