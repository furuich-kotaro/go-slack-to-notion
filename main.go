package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/dstotijn/go-notion"
	"github.com/slack-go/slack"
)

func main() {
	http.HandleFunc("/slack/interaction", slackInteractionHandleFunc)
	http.HandleFunc("/slack/slash_command", slackCommandHandlerFunc)
	http.ListenAndServe("127.0.0.1:8080", nil)
}

func slackCommandHandlerFunc(w http.ResponseWriter, r *http.Request) {
	verifier, err := slack.NewSecretsVerifier(r.Header, os.Getenv("SLACK_SIGNING_SECRET"))
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	bodyReader := io.TeeReader(r.Body, &verifier)
	body, err := ioutil.ReadAll(bodyReader)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := verifier.Ensure(); err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	parameters, err := url.QueryUnescape(string(body))
	if err != nil {
		fmt.Println("failed to unespace query: %w", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	payload := make(map[string]string)
	for _, s := range strings.Split(parameters, "&") {
		arr := strings.Split(s, "=")
		payload[arr[0]] = arr[1]
	}
	inputModal := createInputModal()
	slackClient := slack.New(os.Getenv("SLACK_TOKEN"))
	if _, err := slackClient.OpenView(payload["trigger_id"], *inputModal); err != nil {
		fmt.Println("failed to open modal: %w", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
}

func slackInteractionHandleFunc(w http.ResponseWriter, r *http.Request) {

	verifier, err := slack.NewSecretsVerifier(r.Header, os.Getenv("SLACK_SIGNING_SECRET"))
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	bodyReader := io.TeeReader(r.Body, &verifier)
	body, err := ioutil.ReadAll(bodyReader)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := verifier.Ensure(); err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if r.Method != http.MethodPost {
		fmt.Printf("[ERROR] Invalid method: %s", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err != nil {
		fmt.Printf("[ERROR] Failed to read request body: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	jsonStr, err := url.QueryUnescape(string(body)[8:])
	if err != nil {
		fmt.Printf("[ERROR] Failed to unespace request body: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var message slack.AttachmentActionCallback
	if err := json.Unmarshal([]byte(jsonStr), &message); err != nil {
		fmt.Printf("[ERROR] Failed to decode json message from slack: %s", jsonStr)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	title := message.View.State.Values["notion_title"]["title"].Value
	content := message.View.State.Values["notion_content"]["content"].Value
	addPageToNotionDB(title, content)

	w.Header().Set("Content-Type", "application/json")
}

func createInputModal() *slack.ModalViewRequest {
	titleInputLabel := slack.NewTextBlockObject("plain_text", "タイトル", true, false)
	titleInputElement := slack.NewPlainTextInputBlockElement(titleInputLabel, "title")
	titleInput := slack.NewInputBlock("notion_title", titleInputLabel, titleInputElement)

	contextInputLabel := slack.NewTextBlockObject("plain_text", "タイトル", true, false)
	contextInputElement := slack.NewPlainTextInputBlockElement(contextInputLabel, "context")
	contextInputElement.Multiline = true
	contextInput := slack.NewInputBlock("notion_context", contextInputLabel, contextInputElement)

	inputModal := &slack.ModalViewRequest{
		Type:   slack.ViewType("modal"),
		Title:  slack.NewTextBlockObject("plain_text", "NotionのDBに追加する", true, false),
		Blocks: slack.Blocks{BlockSet: []slack.Block{titleInput, contextInput}},
		Close:  slack.NewTextBlockObject("plain_text", "キャンセル", true, false),
		Submit: slack.NewTextBlockObject("plain_text", "追加", true, false),
	}

	return inputModal
}

func addPageToNotionDB(title string, content string) {
	notionClient := notion.NewClient(os.Getenv("NOTION_TOKEN"))
	ctx := context.Background()
	notionTitle := []notion.RichText{
		{
			Type: notion.RichTextTypeText,
			Text: &notion.Text{Content: title},
		},
	}

	notionContext := []notion.RichText{
		{
			Type: notion.RichTextTypeText,
			Text: &notion.Text{Content: content},
		},
	}

	children := []notion.Block{
		{
			Object:    "block",
			Type:      notion.BlockTypeParagraph,
			Paragraph: &notion.RichTextBlock{Text: notionContext},
		},
	}
	properties := &notion.DatabasePageProperties{
		"title":  notion.DatabasePageProperty{Title: notionTitle},
		"status": notion.DatabasePageProperty{Select: &notion.SelectOptions{Name: "WIP"}},
	}

	params := notion.CreatePageParams{
		ParentID:               os.Getenv("NOTION_DATABASE"),
		ParentType:             notion.ParentTypeDatabase,
		Title:                  notionTitle,
		DatabasePageProperties: properties,
		Children:               children,
	}

	_, err := notionClient.CreatePage(ctx, params)
	if err != nil {
		fmt.Println(err)
		return
	}

	return
}
