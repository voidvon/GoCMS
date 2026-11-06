import { Combobox } from "@base-ui/react/combobox"
import { Check, ChevronsUpDown } from "lucide-react"
import { useTranslation } from "react-i18next"
import { inputVariants } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
export type AddressOption = { id: string; label: string; search?: string; description?: string; disabled?: boolean; formValue?: string }
type Props = {
  id: string
  name: string
  items: AddressOption[]
  value: AddressOption | null
  onChange: (value: AddressOption | null) => void
  onSearch?: (query: string) => void
  required?: boolean
  disabled?: boolean
  loading?: boolean
}
export function AddressSelect({ id, name, items, value, onChange, onSearch, required, disabled, loading }: Props) {
  const { t } = useTranslation()
  return <Combobox.Root<AddressOption> items={items} value={value} onValueChange={onChange} name={name} required={required} disabled={disabled}
    itemToStringLabel={item => item.label} itemToStringValue={item => item.formValue ?? item.id} isItemEqualToValue={(a, b) => a.id === b.id}
    filter={onSearch ? null : (item, query) => `${item.label} ${item.search || ""}`.toLocaleLowerCase().includes(query.toLocaleLowerCase().trim())}
    onInputValueChange={(query, details) => { if (details.reason === "input-change" || details.reason === "input-clear") onSearch?.(query) }}>
    <div className="relative">
      <Combobox.Input id={id} placeholder={t("geography.search")} className={`${inputVariants()} pe-9`} aria-busy={loading} maxLength={100} />
      <Combobox.Trigger className="absolute inset-y-0 end-0 flex w-8 items-center justify-center rounded-e-lg text-muted-foreground disabled:opacity-50" aria-label={t("geography.openOptions")}><ChevronsUpDown className="size-4" /></Combobox.Trigger>
    </div>
    <Combobox.Portal><Combobox.Positioner sideOffset={6} className="z-50 outline-none"><Combobox.Popup className="w-[var(--anchor-width)] min-w-48 max-w-[calc(100vw-2rem)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-md outline-none">
      <div role="status" className="px-2 text-xs text-muted-foreground">{loading ? t("common.loading") : ""}</div>
      <Combobox.Empty className="p-3 text-sm text-muted-foreground">{!loading && t("geography.noResults")}</Combobox.Empty>
      <ScrollArea className="max-h-60"><Combobox.List>{(item: AddressOption) => <Combobox.Item key={item.id} value={item} disabled={item.disabled} className="relative flex cursor-default select-none items-center gap-2 rounded-md py-2 pe-7 ps-2 text-sm outline-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-disabled:opacity-50">
        <span className="min-w-0"><span className="block break-words">{item.label}</span>{item.description && <span className="block text-xs text-muted-foreground">{item.description}</span>}{item.disabled && <span className="block text-xs text-muted-foreground">{t("geography.longName")}</span>}</span>
        <Combobox.ItemIndicator className="absolute end-2"><Check className="size-3.5" /></Combobox.ItemIndicator>
      </Combobox.Item>}</Combobox.List></ScrollArea>
    </Combobox.Popup></Combobox.Positioner></Combobox.Portal>
  </Combobox.Root>
}
