import React from "react"
import ReactDOM from "react-dom/client"
import { ThemeProvider } from "next-themes"
import "./i18n"
import { LocaleProvider } from "./components/app/locale-provider"
import App from "./App"
import "./index.css"
ReactDOM.createRoot(document.getElementById("root")!).render(<React.StrictMode><ThemeProvider attribute="class" forcedTheme="light"><LocaleProvider><App /></LocaleProvider></ThemeProvider></React.StrictMode>)
