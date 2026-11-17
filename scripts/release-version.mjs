export function parseVersion(value) {
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.exec(value)
  if (!match) throw new Error(`版本号 ${value} 不符合 X.Y.Z 格式`)

  const [major, minor, patch] = match.slice(1).map(Number)
  if (![major, minor, patch].every(Number.isSafeInteger)) {
    throw new Error(`版本号 ${value} 超出有效整数范围`)
  }
  return { major, minor, patch }
}

export function formatVersion({ major, minor, patch }) {
  return `${major}.${minor}.${patch}`
}

export function nextVersion(value) {
  const version = parseVersion(value)
  // Historical releases used patch numbers above 20; their next release
  // follows the same carry rule (for example, 0.1.66 -> 0.2.0).
  if (version.minor <= 20 && version.patch < 20) {
    version.patch += 1
  } else if (version.minor < 20) {
    version.minor += 1
    version.patch = 0
  } else {
    if (version.major === Number.MAX_SAFE_INTEGER) throw new Error("主版本号超出有效整数范围")
    version.major += 1
    version.minor = 0
    version.patch = 0
  }
  return formatVersion(version)
}
