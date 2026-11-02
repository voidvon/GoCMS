/* GoCMS consent runtime. Themes classify resources at generation time. */
(() => {
  "use strict"
  if (window.GoCMSConsent) return
  const config = JSON.parse(document.getElementById("gocms-consent-config").textContent)
  const key = "gocms:consent:" + config.site_id
  const optional = ["preferences", "analytics", "marketing"]
  const text = config.text
  let record = null
  let root, panel, details, launcher, closeButton, previousFocus
  const inputs = new Map()
  const activated = new WeakSet()
  let queue = Promise.resolve()
  let expiryTimer

  function valid(value) {
    const now = Date.now()
    return value && value.revision === config.revision && value.version === config.version &&
      Number.isFinite(value.time) && value.time <= now && value.time > now - config.max_age_days * 86400000 &&
      value.choices && optional.every(p => typeof value.choices[p] === "boolean")
  }
  function read() {
    try {
      const value = JSON.parse(localStorage.getItem(key))
      return valid(value) ? value : null
    } catch { return null }
  }
  record = read()
  function watchExpiry() {
    clearTimeout(expiryTimer)
    if (!record) return
    const remaining = record.time + config.max_age_days * 86400000 - Date.now()
    expiryTimer = setTimeout(() => {
      if (!valid(record)) { window.location.reload(); return }
      watchExpiry()
    }, Math.max(1, Math.min(remaining, 2147483647)))
  }
  function has(purpose) {
    return purpose === "necessary" || !!(valid(record) && config.purposes.includes(purpose) && record.choices[purpose])
  }
  function get() {
    return {
      necessary: true,
      preferences: has("preferences"), analytics: has("analytics"), marketing: has("marketing"),
      decided: !!valid(record), version: config.version, revision: config.revision,
      time: valid(record) ? record.time : null,
    }
  }
  function notify() {
    window.dispatchEvent(new CustomEvent("gocms:consent-change", { detail: get() }))
  }
  function resources() {
    document.querySelectorAll("script[data-consent],iframe[data-consent],img[data-consent]").forEach(node => {
      if (activated.has(node) || !has(node.dataset.consent)) return
      activated.add(node)
      if (node.tagName === "SCRIPT") {
        queue = queue.then(() => new Promise(resolve => {
          // A preference may have changed while waiting for a dependency.
          if (!has(node.dataset.consent) || !node.isConnected) { activated.delete(node); resolve(); return }
          const script = document.createElement("script")
          for (const attr of node.attributes) {
            if (attr.name !== "type" && !attr.name.startsWith("data-consent")) script.setAttribute(attr.name, attr.value)
          }
          if (node.dataset.consentType) script.type = node.dataset.consentType
          script.async = node.hasAttribute("async")
          script.textContent = node.textContent
          if (node.dataset.consentSrc) script.src = node.dataset.consentSrc
          const executable = !script.type || /^(module|(?:text|application)\/(?:java|ecma)script)$/i.test(script.type)
          const waits = executable && !(script.noModule && "noModule" in document.createElement("script")) && (!!script.src || script.type === "module")
          script.onload = script.onerror = resolve
          node.after(script)
          if (!waits || script.async) resolve()
        }))
      } else {
        for (const attr of ["srcset", "srcdoc", "src"]) {
          const value = node.getAttribute("data-consent-" + attr)
          if (value !== null) node.setAttribute(attr, value)
        }
      }
    })
  }
  function hide() {
    if (!panel) return
    panel.hidden = true
    launcher.hidden = false
    if (previousFocus && previousFocus.isConnected) previousFocus.focus()
  }
  function open() {
    if (!config.enabled || !panel) return
    previousFocus = document.activeElement
    inputs.forEach((input, p) => { input.checked = has(p) })
    details.hidden = true
    panel.querySelector("[aria-controls='consent-details']").setAttribute("aria-expanded", "false")
    closeButton.hidden = !valid(record)
    panel.hidden = false
    launcher.hidden = true
    panel.focus()
  }
  function choose(choices) {
    const before = get()
    const safeChoices = Object.fromEntries(optional.map(p => [p, config.purposes.includes(p) && choices[p] === true]))
    record = { version: config.version, revision: config.revision, time: Date.now(), choices: safeChoices }
    try { localStorage.setItem(key, JSON.stringify(record)) } catch { /* Keep this page's choice when storage is unavailable. */ }
    watchExpiry()
    notify()
    hide()
    // Reload terminates scripts and embeds that cannot be unloaded safely.
    if (optional.some(p => before[p] && !has(p))) { window.location.reload(); return }
    resources()
  }
  window.GoCMSConsent = Object.freeze({
    get, has, open,
    accept: () => choose(Object.fromEntries(config.purposes.map(p => [p, true]))),
    reject: () => choose({}),
    save: choices => choose(choices || {}),
  })
  document.addEventListener("click", event => {
    const trigger = event.target.closest && event.target.closest("[data-consent-open]")
    if (trigger) { event.preventDefault(); open() }
  })
  window.addEventListener("storage", event => {
    if (event.key !== key && event.key !== null) return
    const before = get()
    record = read()
    watchExpiry()
    notify()
    if (optional.some(p => before[p] && !has(p))) { window.location.reload(); return }
    if (panel && !panel.hidden) inputs.forEach((input, p) => { input.checked = has(p) })
    if (!record) open()
    resources()
  })

  if (config.enabled) {
    const host = document.createElement("gocms-consent")
    document.body.append(host)
    root = host.attachShadow({ mode: "open" })
    const style = document.createElement("style")
    style.textContent = `
      :host{font:14px/1.6 system-ui,sans-serif;color:var(--consent-text,#18222c);position:relative;z-index:2147483000}
      *{box-sizing:border-box}[hidden]{display:none!important}
      .panel{position:fixed;bottom:20px;right:20px;width:min(520px,calc(100vw - 40px));max-height:calc(100dvh - 40px);overflow:auto;scrollbar-width:thin;background:var(--consent-bg,#fff);border:1px solid var(--consent-border,#cbd5df);border-radius:var(--consent-radius,12px);box-shadow:0 8px 40px #0003;padding:24px}
      h2{font:600 20px/1.4 system-ui,sans-serif;margin:0 0 10px}p{margin:0 0 16px}a{color:inherit;text-decoration:underline;text-underline-offset:3px}.links{display:flex;gap:16px;flex-wrap:wrap;margin:12px 0}
      button{font:600 14px/1.5 system-ui,sans-serif;cursor:pointer;border:1px solid var(--consent-border,#cbd5df);border-radius:6px;padding:9px 13px;background:var(--consent-bg,#fff);color:inherit}
      button:focus-visible,a:focus-visible,input:focus-visible,.panel:focus-visible{outline:3px solid var(--consent-accent,#2563eb);outline-offset:3px}
      .actions{display:flex;gap:8px;flex-wrap:wrap}.choice{flex:1;white-space:normal;background:var(--consent-accent,#243f53);color:var(--consent-accent-text,#fff);border-color:var(--consent-accent,#243f53)}
      label{display:flex;gap:10px;align-items:center;padding:10px 0}input{width:18px;height:18px;accent-color:var(--consent-accent,#243f53);flex:none}.details{border-top:1px solid var(--consent-border,#cbd5df);padding-top:12px;margin:16px 0}.necessary{font-size:13px}
      .launcher{position:fixed;bottom:16px;left:16px;box-shadow:0 2px 10px #0002}.close{float:right;padding:4px 8px;margin:0 0 8px 12px}
      @media(max-width:480px){.panel{padding:18px;bottom:12px;right:12px;width:calc(100vw - 24px);max-height:calc(100dvh - 24px)}.actions{flex-direction:column}}
    `
    root.append(style)
    const el = (tag, value, parent, className) => {
      const node = document.createElement(tag)
      if (value) node.textContent = value
      if (className) node.className = className
      if (parent) parent.append(node)
      return node
    }
    const button = (value, parent, handler, className) => {
      const node = el("button", value, parent, className)
      node.type = "button"
      node.setAttribute("part", "button")
      node.addEventListener("click", handler)
      return node
    }
    launcher = button(text.settings, root, open, "launcher")
    launcher.setAttribute("part", "launcher")
    panel = el("section", "", root, "panel")
    panel.hidden = true
    panel.tabIndex = -1
    panel.setAttribute("role", "dialog")
    panel.setAttribute("aria-labelledby", "consent-title")
    panel.setAttribute("aria-describedby", "consent-description")
    panel.setAttribute("part", "panel")
    closeButton = button(text.close, panel, hide, "close")
    const title = el("h2", text.title, panel)
    title.id = "consent-title"
    el("p", text.description, panel).id = "consent-description"
    const links = el("div", "", panel, "links")
    for (const purpose of ["privacy", "cookie"]) {
      if (!text[purpose + "_url"]) continue
      const link = el("a", text[purpose + "_label"], links)
      link.href = text[purpose + "_url"]
    }
    const actions = el("div", "", panel, "actions")
    button(text.accept, actions, window.GoCMSConsent.accept, "choice")
    button(text.reject, actions, window.GoCMSConsent.reject, "choice")
    const customize = button(text.customize, actions, () => {
      details.hidden = !details.hidden
      customize.setAttribute("aria-expanded", String(!details.hidden))
      if (!details.hidden) details.querySelector("input,button").focus()
    })
    customize.setAttribute("aria-expanded", "false")
    customize.setAttribute("aria-controls", "consent-details")
    details = el("div", "", panel, "details")
    details.id = "consent-details"
    details.hidden = true
    el("p", text.necessary, details, "necessary")
    config.purposes.forEach(p => {
      const label = el("label", "", details)
      const input = el("input", "", label)
      input.type = "checkbox"
      input.checked = has(p)
      el("span", text[p], label)
      inputs.set(p, input)
    })
    button(text.save, details, () => choose(Object.fromEntries([...inputs].map(([p, input]) => [p, input.checked]))))
    panel.addEventListener("keydown", event => { if (event.key === "Escape" && valid(record)) hide() })
    if (!record) open()
  }
  // Dynamically added resources must already use inert type/data attributes;
  // this observer does not attempt to undo requests that have already started.
  new MutationObserver(resources).observe(document.documentElement, { childList: true, subtree: true })
  resources()
  watchExpiry()
  notify()
  window.dispatchEvent(new CustomEvent("gocms:consent-ready", { detail: get() }))
})()
