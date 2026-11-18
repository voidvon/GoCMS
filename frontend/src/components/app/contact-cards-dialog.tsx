import { useEffect, useState } from "react"
import { ImageIcon, LoaderCircle, Plus, Trash2 } from "lucide-react"

import { showError, showSuccess } from "@/components/app/admin-notifications"
import { MediaPickerDialog } from "@/components/app/media-picker-dialog"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { getContactCards, saveContactCards, type ContactCard, type ContactCardLibrary, type ContactChannel } from "@/lib/api"

function newCard(): ContactCard {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  const id = Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("")
  return { id, name: "", email: "", phone: "", address: "", channels: [] }
}

export function ContactCardsDialog({ siteId, onClose }: { siteId: number; onClose: () => void }) {
  const [library, setLibrary] = useState<ContactCardLibrary>()
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [mediaTarget, setMediaTarget] = useState<{ cardId: string; index: number } | null>(null)

  useEffect(() => {
    let active = true
    getContactCards(siteId).then(data => {
      if (active) setLibrary(data)
    }).catch(error => {
      if (active) showError(error, "加载联系方式失败")
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false }
  }, [siteId, attempt])

  function updateCards(update: (cards: ContactCard[]) => ContactCard[]) {
    setLibrary(current => current && { ...current, cards: update(current.cards) })
  }

  function updateCard(id: string, update: (card: ContactCard) => ContactCard) {
    updateCards(cards => cards.map(card => card.id === id ? update(card) : card))
  }

  function updateChannel(id: string, index: number, patch: Partial<ContactChannel>) {
    updateCard(id, card => ({
      ...card,
      channels: card.channels?.map((channel, i) => i === index ? { ...channel, ...patch } : channel) ?? [],
    }))
  }

  async function save() {
    if (!library) return
    setSaving(true)
    try {
      await saveContactCards(siteId, library)
      showSuccess("联系方式已保存，静态发布后生效")
      onClose()
    } catch (error) {
      showError(error, "保存联系方式失败")
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <Dialog open onOpenChange={open => { if (!open && !saving) onClose() }}>
        <DialogContent className="flex max-h-[90dvh] flex-col sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>联系方式</DialogTitle>
            <DialogDescription>在此维护联系卡片。在各语言的编辑弹窗中选择要显示的卡片。</DialogDescription>
          </DialogHeader>

          {loading ? (
            <div className="flex min-h-24 items-center justify-center" role="status" aria-label="加载中">
              <LoaderCircle className="size-5 animate-spin" />
            </div>
          ) : !library ? (
            <Button type="button" variant="outline" onClick={() => { setLoading(true); setAttempt(attempt + 1) }}>重新加载</Button>
          ) : (
            <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-4 pr-3">
              {library.cards.map((card, cardIndex) => (
                <fieldset key={card.id} className="space-y-3 rounded-md border p-3" disabled={saving}>
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-sm font-medium">{card.name || `联系卡片 ${cardIndex + 1}`}</span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`移除${card.name || `联系卡片 ${cardIndex + 1}`}`}
                      onClick={() => updateCards(cards => cards.filter(item => item.id !== card.id))}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Label className="grid gap-2">名称
                      <Input value={card.name} onChange={e => updateCard(card.id, item => ({ ...item, name: e.target.value }))} />
                    </Label>
                    <Label className="grid gap-2">邮箱
                      <Input type="email" value={card.email} onChange={e => updateCard(card.id, item => ({ ...item, email: e.target.value }))} />
                    </Label>
                    <Label className="grid gap-2">电话
                      <Input type="tel" value={card.phone} onChange={e => updateCard(card.id, item => ({ ...item, phone: e.target.value }))} />
                    </Label>
                    <Label className="grid gap-2 sm:col-span-2">地址
                      <Textarea rows={2} value={card.address} onChange={e => updateCard(card.id, item => ({ ...item, address: e.target.value }))} />
                    </Label>
                  </div>
                  {(card.channels ?? []).map((channel, index) => (
                    <div key={index} className="space-y-2 rounded-md border p-3">
                      <div className="flex items-end gap-2">
                        <Label className="grid flex-1 gap-2">渠道名称
                          <Input value={channel.label} onChange={e => updateChannel(card.id, index, { label: e.target.value })} />
                        </Label>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`移除渠道 ${channel.label || index + 1}`}
                          onClick={() => updateCard(card.id, item => ({
                            ...item,
                            channels: item.channels?.filter((_, i) => i !== index) ?? [],
                          }))}
                        ><Trash2 /></Button>
                      </div>
                      <Label className="grid gap-2">链接
                        <Input type="url" value={channel.url} onChange={e => updateChannel(card.id, index, { url: e.target.value })} />
                      </Label>
                      <div className="flex flex-wrap items-center gap-2">
                        {channel.qr_url && <img src={channel.qr_url} alt="二维码" className="size-12 rounded border object-contain" />}
                        <Button type="button" variant="outline" size="sm" onClick={() => setMediaTarget({ cardId: card.id, index })}>
                          <ImageIcon />{channel.qr_url ? "更换二维码" : "选择二维码"}
                        </Button>
                        {channel.qr_url && <Button type="button" variant="ghost" size="sm" onClick={() => updateChannel(card.id, index, { qr_url: "" })}>移除二维码</Button>}
                      </div>
                    </div>
                  ))}
                  <Button type="button" variant="outline" size="sm" onClick={() => updateCard(card.id, item => ({
                    ...item,
                    channels: [...(item.channels ?? []), { label: "", url: "", qr_url: "" }],
                  }))}><Plus />添加联络渠道</Button>
                </fieldset>
              ))}
              <Button type="button" variant="outline" size="sm" onClick={() => updateCards(cards => [...cards, newCard()])} disabled={saving}>
                <Plus />添加联系卡片
              </Button>
            </ScrollArea>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={saving}>取消</Button>
            <Button type="button" onClick={() => void save()} disabled={!library || loading || saving}>
              {saving && <LoaderCircle className="size-4 animate-spin" />}保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <MediaPickerDialog
        open={mediaTarget !== null}
        title="选择二维码"
        description="从当前站点的媒体库中选择二维码图片。"
        onOpenChange={open => { if (!open) setMediaTarget(null) }}
        onSelect={asset => {
          if (mediaTarget) updateChannel(mediaTarget.cardId, mediaTarget.index, { qr_url: asset.url })
        }}
      />
    </>
  )
}
