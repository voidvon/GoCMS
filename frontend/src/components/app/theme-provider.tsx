import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react"

import { Moon, Sun } from "lucide-react"

import { IconButton } from "@/components/app/app-ui"
import { ThemeContext, useTheme, type Theme } from "@/components/app/theme-context"

const themeStorageKey = "gocms-admin-theme"

function getSystemTheme(): Theme {
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
}

function getInitialTheme(): Theme {
  try {
    const storedTheme = window.localStorage.getItem(themeStorageKey)
    if (storedTheme === "dark" || storedTheme === "light") {
      return storedTheme
    }
  } catch {
    // Fall back to the system preference when storage is unavailable.
  }

  return getSystemTheme()
}

function applyTheme(theme: Theme) {
  const root = document.documentElement
  root.classList.toggle("dark", theme === "dark")
  root.style.colorScheme = theme

  const themeColor = document.querySelector('meta[name="theme-color"]')
  themeColor?.setAttribute("content", theme === "dark" ? "#171717" : "#ffffff")
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(getInitialTheme)

  const toggleTheme = useCallback(() => {
    setTheme((currentTheme) => (currentTheme === "dark" ? "light" : "dark"))
  }, [])

  useEffect(() => {
    applyTheme(theme)
    try {
      window.localStorage.setItem(themeStorageKey, theme)
    } catch {
      // The theme still applies for this session when storage is unavailable.
    }
  }, [theme])

  useEffect(() => {
    function handleStorage(event: StorageEvent) {
      if (event.key !== themeStorageKey) {
        return
      }
      if (event.newValue === "dark" || event.newValue === "light") {
        setTheme(event.newValue)
      }
    }

    window.addEventListener("storage", handleStorage)
    return () => window.removeEventListener("storage", handleStorage)
  }, [])

  const value = useMemo(
    () => ({
      theme,
      isDark: theme === "dark",
      setTheme,
      toggleTheme,
    }),
    [theme, toggleTheme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function ThemeToggle() {
  const { isDark, toggleTheme } = useTheme()
  const label = isDark ? "切换到日间模式" : "切换到黑夜模式"

  return (
    <IconButton
      variant="ghost"
      size="icon"
      label={label}
      aria-pressed={isDark}
      onClick={toggleTheme}
    >
      {isDark ? <Sun /> : <Moon />}
    </IconButton>
  )
}
