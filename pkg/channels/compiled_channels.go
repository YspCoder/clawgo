package channels

import "sort"

func CompiledChannelKeys() []string {
	out := make([]string, 0, 7)
	if telegramCompiled {
		out = append(out, "telegram")
	}
	if whatsappCompiled {
		out = append(out, "whatsapp")
	}
	if discordCompiled {
		out = append(out, "discord")
	}
	if feishuCompiled {
		out = append(out, "feishu")
	}
	if qqCompiled {
		out = append(out, "qq")
	}
	if dingtalkCompiled {
		out = append(out, "dingtalk")
	}
	if maixcamCompiled {
		out = append(out, "maixcam")
	}
	sort.Strings(out)
	return out
}
