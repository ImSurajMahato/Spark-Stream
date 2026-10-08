package catalog

import (
	"strconv"
	"strings"
)

// Message IDs keep callback data under Telegram's 64-byte limit even for a 64-char slug.
func ParseButton(data string) (string, int, bool) {
	if len(data) < 3 || len(data) > 16 {
		return "", 0, false
	}
	parts := strings.Split(data, ":")
	if len(parts) != 2 || (parts[0] != "s" && parts[0] != "f") {
		return "", 0, false
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil || id <= 0 || id > 2147483647 || strconv.Itoa(id) != parts[1] {
		return "", 0, false
	}
	return parts[0], id, true
}
