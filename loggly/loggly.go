package loggly

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/hsluo/slack-bot"
)

type LogglyAlert struct {
	AlertName        string   `json:"alert_name"`
	AlertDescription string   `json:"alert_description"`
	EditAlertLink    string   `json:"edit_alert_link"`
	SourceGroup      string   `json:"source_group"`
	StartTime        string   `json:"start_time"`
	EndTime          string   `json:"end_time"`
	SearchLink       string   `json:"search_link"`
	Query            string   `json:"query"`
	NumHits          int      `json:"num_hits"`
	RecentHits       []string `json:"recent_hits"`
	OwnerUsername    string   `json:"owner_username"`
}

var (
	rubyErrorRe = regexp.MustCompile(`\w+::\w+`)
)

func FmtHit(hit string) string {
	stackTrace := strings.Split(strings.TrimSpace(hit), "#012")
	lines := make([]string, 0)
	for i := range stackTrace {
		if i == 0 {
			line := rubyErrorRe.ReplaceAllStringFunc(stackTrace[i], func(match string) string {
				return "`" + match + "`"
			})
			lines = append(lines, line)
		} else if i < 6 {
			lines = append(lines, "> "+stackTrace[i])
		} else {
			lines = append(lines, fmt.Sprintf("...and %d lines more", len(stackTrace)-5))
			break
		}
	}
	return strings.Join(lines, "\n")
}

// create slack attachement from Loggly's HTTP alert
func NewAttachment(req *http.Request) (attachment slack.Attachment, err error) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return
	}
	alert := LogglyAlert{}
	if err = json.Unmarshal(body, &alert); err != nil {
		return
	}
	var fallback string
	if strings.Contains(alert.RecentHits[0], "#012") {
		fallback = strings.SplitN(alert.RecentHits[0], "#012", 2)[0]
		for i, hit := range alert.RecentHits {
			alert.RecentHits[i] = FmtHit(hit)
		}
	} else {
		fallback = alert.RecentHits[0]
	}
	fields := []slack.Field{
		{Title: "Description", Value: alert.AlertDescription, Short: false},
		{Title: "Query", Value: alert.Query, Short: true},
		{Title: "Num Hits", Value: strconv.Itoa(alert.NumHits), Short: true},
		{Title: "Recent Hits", Value: strings.Join(alert.RecentHits, "\n"), Short: false},
	}
	attachment = slack.Attachment{
		Fallback:  fallback,
		Color:     "warning",
		Title:     alert.AlertName,
		TitleLink: alert.SearchLink,
		Text:      fmt.Sprintf("Edit this alert on <%s|Loggly>", alert.EditAlertLink),
		Fields:    fields,
		MrkdwnIn:  []string{"fields"},
	}
	return attachment, nil
}

type SearchResult struct {
	TotalEvents int `json:"total_events"`
	Events      []Event
}

type Event struct {
	Tags      []string
	Timestamp int64
	Logmsg    string
	Logtypes  []string
	Id        string
	Event     map[string]interface{}
}

type SubEvent struct {
	Severity  string
	Facility  string
	Timestamp string
	AppName   string
	Pid       int
	Priority  string
	Host      string
}

type Client struct {
	Client   *http.Client
	domain   string
	username string
	password string
	apiBase  string
}

func NewClient(domain, username, password string) *Client {
	return &Client{
		domain:   domain,
		username: username,
		password: password,
		apiBase:  fmt.Sprintf("http://%s.loggly.com/apiv2/", domain),
	}
}

func (c *Client) Get(endpoint string) *LogglyResponse {
	req, _ := http.NewRequest("GET", c.apiBase+endpoint, nil)
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.Client.Do(req)
	if err != nil {
		panic(err)
	}
	return &LogglyResponse{resp}
}

func (c *Client) GetRsid(params url.Values) (string, error) {
	resp := make(map[string]interface{})
	err := c.Get(fmt.Sprintf("search?%s", params.Encode())).decodeJson(&resp)
	if err != nil {
		return "", err
	} else {
		rsid := resp["rsid"].(map[string]interface{})["id"].(string)
		return rsid, nil
	}
}

func (c *Client) GetEvents(rsid string) (*SearchResult, error) {
	searchResult := SearchResult{}
	err := c.Get(fmt.Sprintf("events?rsid=%s", rsid)).decodeJson(&searchResult)
	return &searchResult, err
}

func (c *Client) Search(params url.Values) (*SearchResult, error) {
	rsid, err := c.GetRsid(params)
	if err != nil {
		return nil, err
	}

	return c.GetEvents(rsid)
}

type LogglyResponse struct {
	*http.Response
}

func (r *LogglyResponse) decodeJson(v interface{}) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	for {
		if err := dec.Decode(v); err == io.EOF {
			return nil
		} else {
			return err
		}
	}
}
