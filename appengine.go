// +build appengine

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/context"

	"loggly"

	"github.com/hsluo/slack-bot"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
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

	logglyDomain   = os.Getenv("LOGGLY_DOMAIN")
	logglyUsername = os.Getenv("LOGGLY_USERNAME")
	logglyPassword = os.Getenv("LOGGLY_PASSWORD")
)

func logglyAlert(rw http.ResponseWriter, req *http.Request) {
	c := appengine.NewContext(req)

	attachment, err := loggly.NewAttachment(req)
	if err != nil {
		log.Errorf(c, "%s", err)
		return
	}

	bytes, err := json.Marshal([]slack.Attachment{attachment})
	if err != nil {
		log.Errorf(c, "%s", err)
		return
	}
	data := url.Values{}
	data.Add("channel", "#loggly")
	data.Add("attachments", string(bytes))
	data.Add("as_user", "false")
	outgoing <- task{context: c, method: "chat.postMessage", data: data}
}

func logglySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && r.FormValue("ssl_check") == "1" {
		return
	}

	ctx := appengine.NewContext(r)
	searchResult, err := search(ctx, r.PostFormValue("text"))
	if err != nil {
		http.Error(w, "error", 500)
		log.Errorf(ctx, "failed to search, error: %v", err.Error())
		return
	}

	response := fmtEvents(searchResult.Events)
	fmt.Fprint(w, response)
}

func search(ctx context.Context, query string) (*loggly.SearchResult, error) {
	client := loggly.NewClient(logglyDomain, logglyUsername, logglyPassword)
	client.Client = urlfetch.Client(ctx)

	searchResult, err := client.Search(url.Values{
		"q":     {query},
		"from":  {"-1h"},
		"until": {"now"},
		"size":  {"20"},
		"order": {"asc"},
	})
	if err != nil {
		return nil, err
	}

	return searchResult, nil
}

func fmtEvents(events []loggly.Event) string {
	result := make([]string, 0)
	for _, e := range events {
		var text string
		if v, ok := e.Event["json"]; ok {
			v := v.(map[string]interface{})
			logTime, _ := time.Parse(time.RFC3339Nano, v["time"].(string))
			text = fmt.Sprintf("*%v* %v %v %v u:%v", logTime.Format("01-02 15:04:05.000"), v["method"], v["path"], v["status"], v["user_id"])
		} else {
			text = e.Logmsg
			if strings.Contains(e.Logmsg, "#012") {
				text = loggly.FmtHit(e.Logmsg)
			}
			loc, err := time.LoadLocation("Asia/Shanghai")
			if err != nil {
				loc = time.Local
			}
			t := time.Unix(
				e.Timestamp/1000,
				int64(math.Remainder(float64(e.Timestamp), 1000))*time.Millisecond.Nanoseconds(),
			).In(loc).Format("01-02 15:04:05.000")
			text = fmt.Sprintf("*%v* %s", t, text)
		}
		result = append(result, text)
	}
	return strings.Join(result, "\n")
}

func worker(outgoing <-chan task) {
	for task := range outgoing {
		_, err := bot.WithClient(urlfetch.Client(task.context)).PostForm(task.method, task.data)
		if err != nil {
			log.Errorf(task.context, "%s\n%v", err, task.data)
		}
	}
}

func init() {
	credential, err := slack.LoadCredentials("credentials.json")
	if err != nil {
		panic(err)
	}
	bot = credential.Bot

	outgoing = make(chan task)
	go worker(outgoing)

	http.HandleFunc("/loggly", logglyAlert)
	http.Handle("/loggly/search", slack.ValidateCommand(http.HandlerFunc(logglySearch), credential.Commands))
}

func main() {}
