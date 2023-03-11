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

// http handler function
func main() {
	fmt.Println("[INFO] Start Server")

	// http.HandleFunc("/slack/slash_command", SlackCommandHander)
	http.HandleFunc("/slack/events", slackEventHandler)
	http.ListenAndServe(":80", nil)
}

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

	eventsAPIEvent, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		fmt.Printf("[ERROR] Invalid request signatur: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	personJSON, err := json.Marshal(eventsAPIEvent)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println(string(personJSON))

	if eventsAPIEvent.Type == slackevents.URLVerification {
		var r *slackevents.ChallengeResponse
		err := json.Unmarshal([]byte(body), &r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text")
		w.Write([]byte(r.Challenge))
	}

	switch event := eventsAPIEvent.InnerEvent.Data.(type) {
	case *slackevents.ReactionAddedEvent:
		err := reactionAddedEventHandler(event)
		if err != nil {
			fmt.Printf("[ERROR] Failed to handle ReactionAddedEvent: %v", err)
		}
	default:
		fmt.Printf("[INFO] unknow slackevents: %s", event)
	}
	response := map[string]string{"message": "OK"}
	jsonResponse, _ := json.Marshal(response)

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

func reactionAddedEventHandler(event *slackevents.ReactionAddedEvent) error {
	if event.Reaction == "slack-to-notion" {
		messages, err := getAllMessagesInThread(event)
		if err != nil {
			return err
		}

		if len(messages) != 0 {
			link, err := getMessagePermalink(event.Item.Channel, event.Item.Timestamp)
			if err != nil {
				return err
			}
			if err := addPageToNotionDB(messages, link); err != nil {
				return err
			}
		}
	}
	return nil
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

		if threadMessages[0].Timestamp != threadMessages[0].ThreadTimestamp {
			break
		}

		messages = append(messages, threadMessages...)

		if !hasMore {
			break
		}
		cursor = nextCursor
	}
	return messages, nil
}

func getMessagePermalink(channel string, timestamp string) (string, error) {
	api := slack.New(os.Getenv("SLACK_TOKEN"))

	permalink, err := api.GetPermalink(&slack.PermalinkParameters{
		Channel: channel,
		Ts:      timestamp,
	})
	if err != nil {
		return "", err
	}
	return permalink, nil
}

func addPageToNotionDB(slackMessages []slack.Message, slackLink string) error {
	title := slackMessages[0]
	notionTitle := []notion.RichText{
		{
			Type: notion.RichTextTypeText,
			Text: &notion.Text{Content: title.Text},
		},
	}

	children := []notion.Block{}
	paragraph := notion.Block{
		Object: "block",
		Type:   notion.BlockTypeParagraph,
		Paragraph: &notion.RichTextBlock{
			Text: []notion.RichText{
				{
					Type: notion.RichTextTypeText,
					Text: &notion.Text{
						Content: slackLink,
						Link:    &notion.Link{URL: slackLink},
					},
				},
			},
		},
	}
	children = append(children, paragraph)

	for index, message := range slackMessages {
		var emoji string
		if index == 0 {
			emoji = "❓"
		} else {
			emoji = "📝"
		}

		children = append(children, notion.Block{
			Object: "block",
			Type:   notion.BlockTypeCallout,
			Callout: &notion.Callout{
				RichTextBlock: notion.RichTextBlock{
					Text: []notion.RichText{
						{
							Type: notion.RichTextTypeText,
							Text: &notion.Text{Content: message.Text},
						},
					},
				},
				Icon: &notion.Icon{
					Type:  notion.IconTypeEmoji,
					Emoji: &emoji,
				},
			},
		})
	}

	properties := &notion.DatabasePageProperties{
		"Name": notion.DatabasePageProperty{Title: notionTitle},
	}

	params := notion.CreatePageParams{
		ParentID:               os.Getenv("NOTION_DATABASE"),
		ParentType:             notion.ParentTypeDatabase,
		Title:                  notionTitle,
		DatabasePageProperties: properties,
		Children:               children,
	}

	notionClient := notion.NewClient(os.Getenv("NOTION_TOKEN"))

	_, err := notionClient.CreatePage(context.Background(), params)
	if err != nil {
		return err
	}

	return nil
}
