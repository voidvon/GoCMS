#!/usr/bin/env node

import { spawnSync } from "node:child_process"
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { fileURLToPath } from "node:url"
import { dirname, join, resolve } from "node:path"

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const packagePath = resolve(root, "frontend/package.json")
const lockPath = resolve(root, "frontend/package-lock.json")
const remote = process.env.RELEASE_REMOTE || "origin"
const dryRun = process.argv.includes("--dry-run") || process.env.RELEASE_DRY_RUN === "1"

function fail(message) {
  console.error(`发布失败：${message}`)
  process.exit(1)
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: options.cwd ?? root,
    encoding: "utf8",
    env: options.env,
    stdio: options.capture ? ["ignore", "pipe", "pipe"] : "inherit",
  })

  if (result.error) {
    fail(`${command} 不可用：${result.error.message}`)
  }
  if (result.status !== 0) {
    const detail = options.capture ? result.stderr.trim() : ""
    fail(`${command} ${args.join(" ")} 执行失败${detail ? `：${detail}` : ""}`)
  }
  return result
}

function succeeds(command, args) {
  const result = spawnSync(command, args, {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "ignore", "pipe"],
  })

  if (result.error) {
    fail(`${command} 不可用：${result.error.message}`)
  }
  return result.status === 0
}

function capture(command, args) {
  return run(command, args, { capture: true }).stdout.trim()
}

function readJson(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"))
  } catch (error) {
    fail(`无法读取 ${path}：${error.message}`)
  }
}

function writeJson(path, value) {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`)
}

function parseVersion(value) {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(value)
  if (!match) {
    fail(`版本号 ${value} 不符合 X.Y.Z 格式`)
  }

  const version = match.slice(1).map(Number)
  if (version[0] !== 0 || version[1] < 1 || version[2] > 99) {
    fail(`版本号 ${value} 不符合 0.1.x 的发布规则`)
  }
  return { major: version[0], minor: version[1], patch: version[2] }
}

function formatVersion({ major, minor, patch }) {
  return `${major}.${minor}.${patch}`
}

function nextVersion(value) {
  const version = parseVersion(value)
  if (version.patch < 99) {
    version.patch += 1
  } else {
    version.minor += 1
    version.patch = 0
  }
  return formatVersion(version)
}

function localTagExists(tag) {
  return succeeds("git", ["rev-parse", "--verify", "--quiet", `refs/tags/${tag}`])
}

function remoteTagExists(tag) {
  const result = spawnSync("git", ["ls-remote", "--exit-code", "--tags", remote, `refs/tags/${tag}`], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "ignore", "pipe"],
  })

  if (result.error) {
    fail(`无法检查远程 tag：${result.error.message}`)
  }
  if (result.status === 0) return true
  if (result.status === 2) return false
  fail(`无法检查远程 tag ${tag}：${result.stderr.trim()}`)
}

function releaseExists(tag) {
  return succeeds("gh", ["release", "view", tag, "--json", "tagName"])
}

function updateVersion(version) {
  const packageJson = readJson(packagePath)
  const packageLock = readJson(lockPath)

  packageJson.version = version
  packageLock.version = version
  if (!packageLock.packages?.[""]) {
    fail("frontend/package-lock.json 缺少根 package 信息")
  }
  packageLock.packages[""].version = version

  writeJson(packagePath, packageJson)
  writeJson(lockPath, packageLock)
}

const releaseTargets = [
  ["darwin", "arm64"],
  ["darwin", "amd64"],
  ["linux", "amd64"],
  ["linux", "arm64"],
  ["windows", "amd64"],
]

function createReleaseAssets(tag) {
  const temporaryRoot = mkdtempSync(join(tmpdir(), "gocms-release-"))
  const assets = []
  const backendRoot = resolve(root, "backend")

  for (const [goos, goarch] of releaseTargets) {
    const binarySuffix = goos === "windows" ? ".exe" : ""
    const binaryName = `gocms-${tag}-${goos}-${goarch}${binarySuffix}`
    const binaryPath = join(temporaryRoot, binaryName)
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", binaryPath, "./cmd/site"], {
      cwd: backendRoot,
      env: { ...process.env, CGO_ENABLED: "0", GOOS: goos, GOARCH: goarch },
    })
    assets.push(binaryPath)
  }

  return { assets, temporaryRoot }
}

const packageJson = readJson(packagePath)
const currentVersion = parseVersion(packageJson.version)
const currentTag = `v${formatVersion(currentVersion)}`

if (!dryRun && capture("git", ["status", "--porcelain"])) {
  fail("工作区有未提交改动，请先提交或清理后再发布")
}

if (!succeeds("gh", ["auth", "status"])) {
  fail("gh 未登录，请先执行 gh auth login")
}

const hasCurrentRelease = releaseExists(currentTag)
const hasCurrentLocalTag = localTagExists(currentTag)
const hasCurrentRemoteTag = remoteTagExists(currentTag)

if (!hasCurrentRelease && (hasCurrentLocalTag || hasCurrentRemoteTag)) {
  fail(`${currentTag} 已存在但没有对应 GitHub Release，请先处理这个 tag`)
}

const releaseVersion = hasCurrentRelease ? nextVersion(packageJson.version) : packageJson.version
const releaseTag = `v${releaseVersion}`

if (releaseTag !== currentTag && (releaseExists(releaseTag) || localTagExists(releaseTag) || remoteTagExists(releaseTag))) {
  fail(`${releaseTag} 已存在，无法重复发布`)
}

console.log(`准备发布 ${releaseTag}`)
if (releaseVersion !== packageJson.version) {
  console.log(`版本号：${packageJson.version} -> ${releaseVersion}`)
}

if (dryRun) {
  console.log("试运行结束，没有修改文件、提交、推送或创建 Release")
  process.exit(0)
}

const versionChanged = releaseVersion !== packageJson.version
if (versionChanged) {
  updateVersion(releaseVersion)
}

run("make", ["test"])

const releaseAssets = createReleaseAssets(releaseTag)

if (versionChanged) {
  run("git", ["add", "frontend/package.json", "frontend/package-lock.json"])
  run("git", ["commit", "-m", `chore(release): ${releaseTag}`])
}

const branch = capture("git", ["branch", "--show-current"])
if (!branch) {
  fail("当前处于 detached HEAD，无法自动推送发布提交")
}

run("git", ["tag", "-a", releaseTag, "-m", `Release ${releaseTag}`])
run("git", ["push", remote, branch])
run("git", ["push", remote, releaseTag])
run("gh", ["release", "create", releaseTag, "--verify-tag", "--generate-notes", "--title", releaseTag, ...releaseAssets.assets])
rmSync(releaseAssets.temporaryRoot, { recursive: true, force: true })
console.log(`已发布 GitHub Release ${releaseTag}`)
