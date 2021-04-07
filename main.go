package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

func init() {
	// loads values from .env into the system
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found")
	}
}

func main() {
	ctx := context.TODO()
	uri := os.Getenv("MONGO_URI")
	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)
	collection := client.Database("nft").Collection("opensea_events")

	// OpenSea events API capped at 200 pages at a time
	// Rerunning the task manually appears to be enough delay to reset counter
	for i := 0; i <= 200; i++ {
		events := fetchEvents(i)

		if events == nil {
			fmt.Println("No events returned")
			break
		}
		_, err := collection.InsertMany(ctx, events)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(".")
	}
	getLatestRecord(collection)

	fmt.Println("Program complete")
}

// Don't worry about event structure, we just know it's an array of json objects
type OpenSeaEvents struct {
	Events []interface{} `json:"asset_events"`
}

// Fetch events from the OpenSea API, returning as an array of JSON objects
// Page of API results specified as parameter; starting timestamp is hard-coded
// based on earliest timestamp of previous batch.
// Getting this tinestamp and manually copy-pasting into this URI is a tedious hack
func fetchEvents(page int) []interface{} {
	url := fmt.Sprintf("https://api.opensea.io/api/v1/events?only_opensea=false&offset=%d&limit=50&occurred_before=1615829799&event_type=successful", page*50)
	response, err := http.Get(url)
	if err != nil {
		log.Printf("Request Failed: %s", err)
		return nil
	}
	var events OpenSeaEvents
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Printf("Read Failed: %s", err)
		return nil
	}
	err = json.Unmarshal(body, &events)

	return events.Events
}

// Interface for OpenSea event data
// Only care about date so ignore all other fields
type EventDate struct {
	CreatedDate string `bson:"created_date"`
}

// Print earliest timestamp in Collection
// Note this means you can't change event_type in the OpenSea request between runs, or you'll get different data
func getLatestRecord(collection *mongo.Collection) {

	opts := options.FindOne().SetSort(bson.D{{"created_date", 1}})
	var createdDate EventDate
	err := collection.FindOne(nil, bson.D{}, opts).Decode(&createdDate)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(createdDate.CreatedDate)
}