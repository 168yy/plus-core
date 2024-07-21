package registry

import telebot "github.com/168yy/gfbot"

type BotRegistry struct {
	registry[*telebot.Bot]
}
