import { useSyncExternalStore } from "react"
import { Languages } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select"
import { changeLanguage, currentLanguage, languages, languageSelection, subscribeLanguageSelection } from "@/i18n"
import { showError } from "./feedback"

export function LanguageSwitcher() {
  const { t } = useTranslation()
  const selection = useSyncExternalStore(subscribeLanguageSelection, languageSelection)
  const language = currentLanguage()
  return <Select value={selection} onValueChange={value => { if (value) void changeLanguage(value).catch(showError) }}>
    <SelectTrigger aria-label={t("common.language")} className="h-10 min-w-28 max-w-48 gap-2 border-transparent bg-muted/60 sm:min-w-32">
      <Languages aria-hidden="true" className="size-4" />
      <SelectValue className="text-start">{selection === "follow" ? t("common.followSiteLanguage") : <span lang={language} dir="auto">{languages.find(item => item.code === language)?.name}</span>}</SelectValue>
    </SelectTrigger>
    <SelectContent align="end">
      <SelectItem value="follow">{t("common.followSiteLanguage")}</SelectItem>
      {languages.map(item => <SelectItem key={item.code} value={item.code}><span lang={item.code} dir={item.direction}>{item.name}</span></SelectItem>)}
    </SelectContent>
  </Select>
}
