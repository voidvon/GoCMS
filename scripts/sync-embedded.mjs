#!/usr/bin/env node

import { cpSync, existsSync, mkdirSync, rmSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { dirname, join, resolve } from "node:path"

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const embeddedRoot = resolve(root, "backend/internal/embedded/assets")
const sources = [
  [resolve(root, "frontend/dist"), join(embeddedRoot, "frontend/dist")],
  [resolve(root, "backend/templates"), join(embeddedRoot, "templates")],
]

for (const [source, destination] of sources) {
  if (!existsSync(source)) {
    console.error(`嵌入资源不存在：${source}`)
    process.exit(1)
  }
  rmSync(destination, { recursive: true, force: true })
  mkdirSync(destination, { recursive: true })
  cpSync(source, destination, { recursive: true })
}

console.log("已同步前端和默认模板到 Go 嵌入资源目录")
