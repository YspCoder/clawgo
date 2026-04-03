package channels

import "sort"

func CompiledChannelKeys() []string {
	out := []string{"weixin", "telegram", "feishu"}
	sort.Strings(out)
	return out
}
