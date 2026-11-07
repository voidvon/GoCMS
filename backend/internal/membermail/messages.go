package membermail

import "strings"

// Security mail uses a fixed translation selected by the storefront language,
// never caller-supplied subject/body content.
func ResetMessage(language, link string) (string, string) {
	messages := map[string][2]string{
		"en": {"Reset your password", "Use the link below to reset your password. It expires in 30 minutes and can be used once. If you did not request this, ignore this email."},
		"zh": {"重置密码", "请使用下方链接重置密码。链接 30 分钟内有效且只能使用一次。如果不是您本人操作，请忽略此邮件。"},
		"es": {"Restablecer contraseña", "Utiliza el enlace para restablecer tu contraseña. Caduca en 30 minutos y solo se puede usar una vez. Si no lo solicitaste, ignora este correo."},
		"fr": {"Réinitialiser le mot de passe", "Utilisez le lien pour réinitialiser votre mot de passe. Il expire dans 30 minutes et ne peut être utilisé qu’une fois. Ignorez ce message si vous n’êtes pas à l’origine de la demande."},
		"pt": {"Redefinir senha", "Use o link para redefinir sua senha. Ele expira em 30 minutos e só pode ser usado uma vez. Se não fez esta solicitação, ignore este e-mail."},
		"ru": {"Сброс пароля", "Используйте ссылку для сброса пароля. Она действует 30 минут и может быть использована один раз. Если вы не запрашивали сброс, проигнорируйте письмо."},
		"ar": {"إعادة تعيين كلمة المرور", "استخدم الرابط لإعادة تعيين كلمة المرور. تنتهي صلاحيته خلال 30 دقيقة ويمكن استخدامه مرة واحدة. تجاهل الرسالة إذا لم تطلب ذلك."},
		"id": {"Atur ulang kata sandi", "Gunakan tautan untuk mengatur ulang kata sandi. Tautan berlaku 30 menit dan hanya dapat digunakan sekali. Abaikan email ini jika Anda tidak memintanya."},
		"vi": {"Đặt lại mật khẩu", "Dùng liên kết để đặt lại mật khẩu. Liên kết có hiệu lực trong 30 phút và chỉ dùng được một lần. Nếu bạn không yêu cầu, hãy bỏ qua email này."},
		"tr": {"Parolanızı sıfırlayın", "Parolanızı sıfırlamak için bağlantıyı kullanın. Bağlantı 30 dakika geçerlidir ve yalnızca bir kez kullanılabilir. Bu isteği siz yapmadıysanız e-postayı yok sayın."},
		"th": {"รีเซ็ตรหัสผ่าน", "ใช้ลิงก์เพื่อรีเซ็ตรหัสผ่าน ลิงก์มีอายุ 30 นาทีและใช้ได้ครั้งเดียว หากคุณไม่ได้ร้องขอ โปรดเพิกเฉยต่ออีเมลนี้"},
	}
	key := strings.ToLower(strings.Split(language, "-")[0])
	m, ok := messages[key]
	if !ok {
		m = messages["en"]
	}
	return m[0], m[1] + "\n\n" + link + "\n"
}

func ShipmentMessage(language, order, details string) (string, string) {
	messages := map[string][2]string{
		"en": {"Order delivery update", "Your order has a new shipment or delivery record. View your order for the delivered items and details."},
		"zh": {"订单发货与交付通知", "您的订单有新的发货或交付记录，请进入订单查看本次交付的商品与详情。"},
		"es": {"Actualización de entrega", "Tu pedido tiene un nuevo envío o entrega. Consulta el pedido para ver los artículos entregados y los detalles."},
		"fr": {"Mise à jour de livraison", "Votre commande comporte une nouvelle expédition ou livraison. Consultez la commande pour connaître les articles livrés et les détails."},
		"pt": {"Atualização de entrega", "Seu pedido tem um novo envio ou entrega. Consulte o pedido para ver os itens entregues e os detalhes."},
		"ru": {"Обновление доставки заказа", "Для вашего заказа добавлена запись об отправке или передаче. Список товаров и подробности доступны в заказе."},
		"ar": {"تحديث تسليم الطلب", "تمت إضافة شحنة أو تسليم لطلبك. راجع الطلب للاطلاع على المنتجات المسلّمة والتفاصيل."},
		"id": {"Pembaruan pengiriman pesanan", "Pesanan Anda memiliki catatan pengiriman atau penyerahan baru. Lihat pesanan untuk barang yang diserahkan dan detailnya."},
		"vi": {"Cập nhật giao hàng", "Đơn hàng có lịch sử gửi hoặc bàn giao mới. Xem đơn hàng để biết sản phẩm đã bàn giao và chi tiết."},
		"tr": {"Sipariş teslimat güncellemesi", "Siparişinize yeni bir gönderi veya teslimat kaydı eklendi. Teslim edilen ürünler ve ayrıntılar için siparişinizi görüntüleyin."},
		"th": {"อัปเดตการจัดส่งคำสั่งซื้อ", "คำสั่งซื้อของคุณมีรายการจัดส่งหรือส่งมอบใหม่ ดูคำสั่งซื้อเพื่อตรวจสอบสินค้าและรายละเอียด"},
	}
	key := strings.ToLower(strings.Split(language, "-")[0])
	m, ok := messages[key]
	if !ok {
		m = messages["en"]
	}
	return m[0], m[1] + "\n\n" + order + "\n\n" + details + "\n"
}
