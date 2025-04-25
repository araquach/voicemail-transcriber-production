package main

import (
	"net/http"
	"voicemail-transcriber-production/internal/gmail"
)

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	gmail.PubSubHandler(w, r)
}
