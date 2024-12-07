package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/sharing"
	"github.com/joho/godotenv"
	"github.com/nfnt/resize"
	"github.com/streadway/amqp"
)

var dropboxConfig dropbox.Config

func init() {

	if err := godotenv.Load(); err != nil {
		panic("Error loading .env file")
	}

	// Set Dropbox token from env
	dropboxConfig = dropbox.Config{
		Token:    os.Getenv("DROPBOX_ACCESS_TOKEN"),
		LogLevel: dropbox.LogInfo,
	}

	// Establish RabbitMQ connection
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		panic("Failed to connect to RabbitMQ: " + err.Error())
	}

	ch, err := conn.Channel()
	if err != nil {
		panic("Failed to open a channel: " + err.Error())
	}

	// Declare a queue
	_, err = ch.QueueDeclare(
		"image_queue", // name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		panic("Failed to declare a queue: " + err.Error())
	}

	// Start consuming
	go consumeImages(ch)
}

func consumeImages(ch *amqp.Channel) {
	msgs, err := ch.Consume(
		"image_queue", // queue
		"",            // consumer
		true,          // auto-ack
		false,         // exclusive
		false,         // no-local
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		panic("Failed to register a consumer: " + err.Error())
	}

	for msg := range msgs {
		imageURL := string(msg.Body)
		fmt.Println("Processing image:", imageURL)

		err := processImage(imageURL)
		if err != nil {
			fmt.Println("Failed to process image:", err)
		} else {
			fmt.Println("Image processed and saved successfully:", imageURL)
		}
	}
}

func processImage(imageURL string) error {
	// Fetch the image from the URL
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(imageURL)
	if err != nil {
		return fmt.Errorf("failed to fetch image: %v", err)
	}
	defer resp.Body.Close()

	// Decode the image
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decode image: %v", err)
	}

	// Resize the image
	resized := resize.Resize(300, 300, img, resize.Lanczos3)

	// Create a temporary buffer to store the resized image
	var buffer bytes.Buffer
	err = jpeg.Encode(&buffer, resized, &jpeg.Options{Quality: 80})
	if err != nil {
		return fmt.Errorf("failed to encode image: %v", err)
	}

	// Set up Dropbox client
	dropboxClient := files.New(dropboxConfig)

	// Set the path to upload to Dropbox
	path := "/" + sanitizeFileName(extractFileName(imageURL))

	// Create the UploadArg
	uploadArg := files.NewUploadArg(path)
	uploadArg.Mode = &files.WriteMode{Tagged: dropbox.Tagged{
		Tag: "overwrite", // This sets the mode to overwrite
	}}

	// Upload the image to Dropbox
	_, err = dropboxClient.Upload(uploadArg, &buffer)
	if err != nil {
		return fmt.Errorf("failed to upload image to Dropbox: %v", err)
	}

	// Generate the sharable link
	sharingClient := sharing.New(dropboxConfig)
	linkArg := &sharing.CreateSharedLinkWithSettingsArg{
		Path: path,
	}
	linkMetadata, err := sharingClient.CreateSharedLinkWithSettings(linkArg)
	if err != nil {
		return fmt.Errorf("failed to create sharable link: %v", err)
	}

	// Extract the sharable URL
	if link, ok := linkMetadata.(*sharing.FileLinkMetadata); ok {
		fmt.Println("Sharable link:", link.Url)
	} else {
		return fmt.Errorf("unexpected metadata type")
	}

	return nil
}

// Helper function to sanitize file names
func sanitizeFileName(fileName string) string {
	// Remove query parameters and sanitize the filename
	parsedURL := regexp.MustCompile(`[^\w\s-]`).ReplaceAllString(fileName, "_") // Replace special chars with _
	parsedURL = regexp.MustCompile(`[\s-]+`).ReplaceAllString(parsedURL, "_")   // Replace spaces with underscores
	parsedURL = strings.Trim(parsedURL, "_")                                    // Trim leading/trailing underscores
	parsedURL = parsedURL + ".jpg"                                              // Ensure it has a valid extension
	return parsedURL
}

// Helper function to extract the file name from the URL
func extractFileName(url string) string {
	tokens := strings.Split(url, "/")
	return tokens[len(tokens)-1]
}

// Main function to start the image processing service
func main() {
	// Print a simple message
	fmt.Println("Starting image processing service...")

	// The service will now listen for image processing tasks from the RabbitMQ queue.
	// init() will take care of starting the RabbitMQ connection and consuming messages.
	select {} // This keeps the main function running indefinitely
}
