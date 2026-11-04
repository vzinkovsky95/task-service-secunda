// IP клиента. Заголовку X-Forwarded-For можно верить, только если запрос пришёл
// от доверенного прокси из настройки TRUSTED_PROXIES: иначе любой клиент
// подставит чужой адрес и обойдёт ограничение. Цепочка читается справа налево,
// доверенные прокси пропускаются, первый недоверенный адрес и есть клиент.
// Нечитаемый элемент цепочки не угадывается: запрос относится к адресу, от
// которого он пришёл. IPv6 приводится к подсети /64, иначе одному клиенту
// достаточно перебирать адреса внутри своей подсети.

package ratelimit

import (
	"net/http"
	"net/netip"
	"strings"
)

const ipv6SubnetBits = 64

func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	peer := remote.Addr().Unmap()

	if !isTrusted(peer, trusted) {
		return normalize(peer)
	}

	chain := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(chain) - 1; i >= 0; i-- {
		entry := strings.TrimSpace(chain[i])
		if entry == "" {
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return normalize(peer)
		}
		addr = addr.Unmap()
		if !isTrusted(addr, trusted) {
			return normalize(addr)
		}
	}
	return normalize(peer)
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func normalize(addr netip.Addr) string {
	if addr.Is6() {
		return netip.PrefixFrom(addr, ipv6SubnetBits).Masked().String()
	}
	return addr.String()
}
