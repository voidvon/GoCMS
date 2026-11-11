import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

export function ImageOptimizationFields({ settings, onChange }: {
  settings: Record<string, string>
  onChange: (settings: Record<string, string>) => void
}) {
  const enabled = settings.image_optimization_enabled !== "false"
  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3.5">
      <div className="flex items-center justify-between gap-4">
        <Label htmlFor="image-optimization-enabled">上传图片自动优化</Label>
        <Switch id="image-optimization-enabled" checked={enabled} onCheckedChange={(checked) => onChange({ ...settings, image_optimization_enabled: String(checked) })} />
      </div>
      <p className="text-xs text-muted-foreground">比较原图与 WebP、JPEG、PNG 的大小，至少节省 512 字节才替换。透明图片保留透明效果；GIF、动画 WebP 和 APNG 保留原样。</p>
      <div className="space-y-1.5">
        <Label htmlFor="image-optimization-quality">压缩质量（1–100）</Label>
        <Input id="image-optimization-quality" type="number" min={1} max={100} step={1} required className="max-w-32" value={settings.image_optimization_quality ?? "80"} onChange={(event) => onChange({ ...settings, image_optimization_quality: event.target.value })} />
        <p className="text-xs text-muted-foreground">默认 80。数值越高，画质通常越好，文件也越大。WebP 和 JPEG 为有损压缩，PNG 使用无损压缩。</p>
      </div>
      <p className="text-xs text-muted-foreground">仅影响本站之后上传的图片，保存后立即生效。支持 JPEG、PNG、GIF、WebP 和 HEIC/HEIF；HEIC/HEIF 始终转为浏览器可显示的格式，关闭自动优化时仍会转换。</p>
    </div>
  )
}
