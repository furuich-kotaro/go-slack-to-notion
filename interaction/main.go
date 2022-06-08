package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/dstotijn/go-notion"
	"github.com/slack-go/slack"
)

type Response events.APIGatewayProxyResponse

func Handler(r events.APIGatewayProxyRequest) (Response, error) {

	if err := verify(r); err != nil {
		log.Printf("[ERROR] failed to verify payload: %v", err)
		return Response{StatusCode: 200}, nil
	}

	body, err := base64.StdEncoding.DecodeString(r.Body)
	if err != nil {
		log.Printf("[ERROR] Failed to decode base64 encoded payload: %v", err)
		return Response{StatusCode: 200}, nil
	}

	parameters, err := url.QueryUnescape(string(body)[8:])
	if err != nil {
		log.Printf("[ERROR] Failed to decode unescape message from slack: %v", err)
		return Response{StatusCode: 200}, nil
	}

	var message slack.AttachmentActionCallback
	if err := json.Unmarshal([]byte(parameters), &message); err != nil {
		log.Printf("[ERROR] Failed to decode json message from slack: %v", err)
		return Response{StatusCode: 200}, nil
	}

	title := message.View.State.Values["notion_title"]["title"].Value
	content := message.View.State.Values["notion_content"]["content"].Value

	if err := addPageToNotionDB(title, content); err != nil {
		log.Printf("[ERROR] Failed to add page to Notion: %v", err)
		return Response{StatusCode: 200}, nil
	}

	resp := Response{
		StatusCode:      200,
		IsBase64Encoded: false,
		Headers: map[string]string{
			"Content-Type":           "application/json",
			"X-MyCompany-Func-Reply": "slack-interaction-handler",
		},
	}

	return resp, nil
}

func verify(r events.APIGatewayProxyRequest) error {
	body, _ := base64.StdEncoding.DecodeString(r.Body)

	header := http.Header{}
	for k, v := range r.Headers {
		header.Set(k, v)
	}

	sv, err := slack.NewSecretsVerifier(header, os.Getenv("SLACK_SIGNING_SECRET"))
	if err != nil {
		return err
	}

	sv.Write(body)
	return sv.Ensure()
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

func main() {
	lambda.Start(Handler)
}
