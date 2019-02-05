package main

import (
	"fmt"
	"github.com/blevesearch/bleve"
	"github.com/docopt/docopt-go"
	"github.com/gelembjuk/articletext"
	"github.com/motemen/go-pocket/api"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
)

var version = "0.1"

var configDir string
var tags []string

func init() {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}

	configDir = filepath.Join(usr.HomeDir, ".config", "pocket")
	err = os.MkdirAll(configDir, 0777)
	if err != nil {
		panic(err)
	}

	tags = allTags()
}

func main() {
	usage := `A Pocket <getpocket.com> client.

Usage:
  pocket [--all] 

Options for list:
  -a, --all <template> A Go template to show items.
`

	_, err := docopt.Parse(usage, nil, true, version, false)
	if err != nil {
		panic(err)
	}

	consumerKey := getConsumerKey()

	accessToken, err := restoreAccessToken(consumerKey)
	if err != nil {
		panic(err)
	}

	client := api.NewClient(consumerKey, accessToken.AccessToken)
	options := &api.RetrieveOption{
		State: api.StateAll,
		DetailType: api.DetailTypeComplete,
	}
	res, err := client.Retrieve(options)
	if err != nil {
		panic(err)
	}
	commandList(client, res)
}

func commandList(client *api.Client, res *api.RetrieveResult) {

	tags = loadTags(res)

	var items []api.Item
	for _, item := range res.List {
		//if item.Status == api.ItemStatusUnread {
		items = append(items, item)
		//}
	}

	count := len(items)
	for i, item := range items {
		newtags := predictTags(item)
		fmt.Printf("%d/%d\t%s:\t", i+1, count, item.Title())
		for tag := range item.Tags {
			fmt.Printf("%s ", tag)
		}
		for _, nt := range newtags {
			fmt.Printf("+%s ", nt)
		}
		fmt.Println()
		AddTags(client, NewAddTagAction(item.ItemID, newtags))
	}
}

func loadTags(res *api.RetrieveResult) []string {
	te := make(map[string]bool)
	for _, item := range res.List {
		for tag := range item.Tags {
			te[strings.Trim(tag, "\"")] = true
		}
	}
	tags := make([]string, len(te))
	for tag := range te {
		tags = append(tags, tag)
	}
	return tags
}

func predictTags(item api.Item) []string {
	mapping := bleve.NewIndexMapping()
	index, err := bleve.NewMemOnly(mapping)
	response, err := http.Get(item.URL())
	fullText := item.Title()
	if nil != err {
		fmt.Printf("error: %s\n", err.Error())
	} else {
		text, _ := articletext.GetArticleText(response.Body)
		fullText = fmt.Sprintf("%s %s", item.Title(), text)
		defer response.Body.Close()
	}

	err = index.Index(string(item.ItemID), fullText)
	if err != nil {
		panic(err)
	}

	ml := MatchesList{}
	i := 0
	for _, tag := range tags {
		tag := strings.Trim(tag, "\"")
		query := bleve.NewMatchQuery(tag)
		search := bleve.NewSearchRequest(query)
		searchResults, err := index.Search(search)
		if err != nil {
			panic(err)
		}
		if searchResults.MaxScore > 0.01 {
			ml = append(ml, Match{tag, searchResults.MaxScore})
			i++
		}
	}
	if 0 == len(ml) {
		return []string{}
	}
	sort.Sort(sort.Reverse(ml))
	min := 2
	if len(ml) < min {
		min = len(ml)
	}
	nt := make(map[string]bool)
	for _, v := range ml[0:min]{
		nt[v.Key] = true
	}
	for ot := range item.Tags {
		nt[strings.Trim(ot, "\"")] = true
	}
	var tags []string
	for tag := range nt {
		tags = append(tags, tag)
	}
	return tags
}

type Match struct {
	Key   string
	Value float64
}

type MatchesList []Match

func (p MatchesList) Len() int           { return len(p) }
func (p MatchesList) Less(i, j int) bool { return p[i].Value < p[j].Value }
func (p MatchesList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

type ReplaceTagsAction struct {
	Action string `json:"action"`
	ItemID int    `json:"item_id,string"`
	Tags string `json:"tags"`
}

func NewAddTagAction(itemID int, tags []string) ReplaceTagsAction {
	return ReplaceTagsAction{
		Action:"tags_replace",
		ItemID:itemID,
		Tags:strings.Join(tags, ","),
	}
}

type modifyAPIOptionsWithAuth struct {
	ConsumerKey string          `json:"consumer_key"`
	AccessToken string          `json:"access_token"`
	Actions []ReplaceTagsAction `json:"actions"`
}

// AddTags requests bulk modification on items.
func AddTags(client *api.Client, action ReplaceTagsAction) {
	if 0 == len(action.Tags) {
		return
	}
	res := &api.ModifyResult{}
	data := modifyAPIOptionsWithAuth{
		ConsumerKey: client.ConsumerKey,
		AccessToken: client.AccessToken,
		Actions: []ReplaceTagsAction{action},
	}
	err := api.PostJSON("/v3/send", data, res)
	if err != nil {
		fmt.Printf("Add tag error: %s", err.Error())
	}
}
