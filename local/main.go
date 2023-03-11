package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/dstotijn/go-notion"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

func slackEventHandler(w http.ResponseWriter, r *http.Request) {
	if err := slackRequestVerifier(r); err != nil {
		fmt.Printf("[ERROR] failed to verify payload: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("[ERROR] Failed to read request payload: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	innerEvent := event.InnerEvent
	switch ev := innerEvent.Data.(type) {
	case *slackevents.ReactionAddedEvent:
		fmt.Printf("[INFO] Channel: %s", ev.Item.Channel)

		fmt.Println("Messages in thread: START")
		messages, err := getAllMessagesInThread(ev)
		if err != nil {
			fmt.Printf("[ERROR] Failed to getAllMessagesInThread: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		fmt.Println("Messages in thread:")
		for _, message := range messages {
			fmt.Println(message.Text)
		}
	default:
		fmt.Printf("[ERROR] unkokn ReactionAddedEvent: %s", ev)
		w.WriteHeader(http.StatusOK)
		return
	}

	// title := message.View.State.Values["notion_title"]["title"].Value
	// content := message.View.State.Values["notion_content"]["content"].Value

	// if err := addPageToNotionDB(title, content); err != nil {
	// 	fmt.Printf("[ERROR] Failed to add page to Notion: %v", err)
	// 	w.WriteHeader(http.StatusOK)
	// 	return
	// }

	response := map[string]string{"message": "OK"}
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		// If there was an error creating the response, return an HTTP error
		http.Error(w, "Error creating JSON response", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}

func slackRequestVerifier(r *http.Request) error {
	headers := r.Header

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	// Replace the body in the request so that it can be read later by the Handler function
	r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	sv, err := slack.NewSecretsVerifier(headers, os.Getenv("SLACK_SIGNING_SECRET"))
	if err != nil {
		return err
	}

	_, err = sv.Write(body)
	if err != nil {
		return err
	}

	return sv.Ensure()
}

/* get all messages in thread by slack.ReactionAddedEvent */
func getAllMessagesInThread(event *slackevents.ReactionAddedEvent) ([]slack.Message, error) {
	api := slack.New(os.Getenv("SLACK_TOKEN"))

	var messages []slack.Message
	var cursor string
	for {
		params := &slack.GetConversationRepliesParameters{
			ChannelID: event.Item.Channel,
			Timestamp: event.Item.Timestamp,
			Limit:     1000,
			Cursor:    cursor,
		}
		threadMessages, hasMore, nextCursor, err := api.GetConversationReplies(params)
		if err != nil {
			return messages, err
		}
		messages = append(messages, threadMessages...)

		if !hasMore {
			break
		}
		cursor = nextCursor
	}
	return messages, nil
}

func addPageToNotionDB(title string, content string) error {
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
		return err
	}

	return nil
}

// http handler function
func main() {
	// http.HandleFunc("/slack/slash_command", SlackCommandHander)
	http.HandleFunc("/slack/events", slackEventHandler)
	http.ListenAndServe(":80", nil)
}
