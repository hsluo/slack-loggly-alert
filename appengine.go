// +build appengine

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/hsluo/slack-bot"

	"google.golang.org/appengine"
	l "google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

type task struct {
	context context.Context
	method  string
	data    url.Values
}

var (
	outgoing chan task
	bot      slack.Bot
)

func logglyAlert(rw http.ResponseWriter, req *http.Request) {
	c := appengine.NewContext(req)

	attachment, err := NewAttachment(req)
	if err != nil {
		l.Errorf(c, "%s", err)
		return
	}

	bytes, err := json.Marshal([]slack.Attachment{attachment})
	if err != nil {
		l.Errorf(c, "%s", err)
		return
	}
	data := url.Values{}
	data.Add("channel", "#loggly")
	data.Add("attachments", string(bytes))
	data.Add("as_user", "false")
	outgoing <- task{context: c, method: "chat.postMessage", data: data}
}

var (
	domain       string
	logglyClient *LogglyClient
)

func logglySearch(rw http.ResponseWriter, req *http.Request) {
	ctx := appengine.NewContext(req)

	if logglyClient == nil {
		logglyClient = &LogglyClient{
			Domain:   os.Getenv("LOGGLY_DOMAIN"),
			Username: os.Getenv("LOGGLY_USERNAME"),
			Password: os.Getenv("LOGGLY_PASSWORD"),
		}
	}
	logglyClient.Client = urlfetch.Client(ctx)

	rsid, err := logglyClient.GetRsid(url.Values{
		"q":     {os.Getenv("LOGGLY_SEARCH_QUERY")},
		"from":  {"-10m"},
		"order": {"asc"},
	})
	if err != nil {
		http.Error(rw, err.Error(), 500)
		return
	}

	searchResult, err := logglyClient.Search(rsid)
	if err != nil {
		http.Error(rw, err.Error(), 500)
		return
	}

	l.Infof(ctx, "rsid=%v events=%v", rsid, searchResult.TotalEvents)

	if searchResult.TotalEvents == 0 {
		return
	}

	outgoing <- task{
		context: ctx,
		method:  "chat.postMessage",
		data: url.Values{
			"channel": {"#loggly"},
			"text":    {fmtEvents(searchResult.Events)},
			"as_user": {"false"},
		},
	}
}

func fmtEvents(events []Event) string {
	result := make([]string, 0)
	for _, e := range events {
		var text string
		if v, ok := e.Event["json"]; ok {
			b, _ := json.MarshalIndent(v, "", "  ")
			text = fmt.Sprintf("```\n%s\n```", string(b))
		} else {
			text = e.Logmsg
			if strings.Contains(e.Logmsg, "#012") {
				text = fmtHit(e.Logmsg)
			}
			loc, err := time.LoadLocation(os.Getenv("LOCATION"))
			if err != nil {
				loc = time.Local
			}
			t := time.Unix(e.Timestamp/1000, 0).In(loc)
			text = fmt.Sprintf("*%v*\n%s", t, text)
		}
		result = append(result, text)
	}
	return strings.Join(result, "\n"+strings.Repeat("=", 100)+"\n")
}

func worker(outgoing <-chan task) {
	for task := range outgoing {
		_, err := bot.WithClient(urlfetch.Client(task.context)).PostForm(task.method, task.data)
		if err != nil {
			l.Errorf(task.context, "%s\n%v", err, task.data)
		}
	}
}

func init() {
	log.Println("appengine init")

	credential, err := slack.LoadCredentials("credentials.json")
	if err != nil {
		log.Fatal(err)
	}
	bot = credential.Bot

	outgoing = make(chan task)
	go worker(outgoing)

	http.HandleFunc("/loggly", logglyAlert)
	http.HandleFunc("/loggly/search", logglySearch)
}

func main() {}