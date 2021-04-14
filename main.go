package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
	db := os.Getenv("MONGO_DATABASE")
	coll := os.Getenv("MONGO_COLLECTION")
	timestamp := os.Getenv("OCCURRED_BEFORE_DATE")

	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)
	collection := client.Database(db).Collection(coll)

	// OpenSea events API capped at 200 pages at a time
	// Rerunning the task manually after updating timestamp environment variable appears to be enough delay to reset counter
	for i := 0; i <= 200; i++ {
		events := fetchEvents(i, timestamp)

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
	removeDuplicates(collection)
	getLatestRecord(collection)

	fmt.Println("Program complete")
}

// Interface for OpenSea Event API response
// Don't worry about event structure, we just know it's an array of json objects
type OpenSeaEvents struct {
	Events []interface{} `json:"asset_events"`
}

// Fetch events from the OpenSea API, returning as an array of JSON objects
// Parameters:
// page - page of API results to return
// time - earliest timestamp found in previous batch
func fetchEvents(page int, time string) []interface{} {
	url := fmt.Sprintf(
		"https://api.opensea.io/api/v1/events?only_opensea=false&offset=%d&limit=50&occurred_before=%s&event_type=successful",
		page*50,
		time)
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

// Interface for OpenSea event data from Mongo
// Only care about date so ignore all other fields
type EventDate struct {
	CreatedDate string `bson:"created_date"`
}

// Print earliest timestamp in Collection to be pasted in as timestamp value for next run
// Note this means you can't change event_type in the OpenSea request between runs, or you'll get different data
func getLatestRecord(collection *mongo.Collection) {

	opts := options.FindOne().SetSort(bson.D{{"created_date", 1}})
	var createdDate EventDate
	err := collection.FindOne(nil, bson.D{}, opts).Decode(&createdDate)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(createdDate.CreatedDate)
	timestamp, err := time.Parse("2006-01-02T15:04:05.999999999", createdDate.CreatedDate)
	if err != nil {
		log.Print(err)
	}
	fmt.Println(timestamp.Unix())
}

type Duplicates struct {
	Dups []primitive.ObjectID `bson:"dups"`
}

// Since updating timestamp is manual and approximate, sometimes duplicate records may be added
// Remove them from Mongo to keep data clean
func removeDuplicates(collection *mongo.Collection) {
	ctx := context.TODO()
	// Group transactions by OpenSea ID. Store MongoDB IDs and count of matching transactions for each ID
	groupStage := bson.D{{
		"$group",
		bson.D{
			{"_id",
				"$id"},
			{"dups",
				bson.D{{
					"$addToSet",
					"$_id"}}},
			{"count",
				bson.D{{
					"$sum",
					1}}}}}}
	// Only match transactions that appear more than once in the database
	matchStage := bson.D{{
		"$match",
		bson.D{
			{"count",
				bson.D{{
					"$gt",
					1}}}}}}

	cursor, err := collection.Aggregate(ctx, mongo.Pipeline{groupStage, matchStage})
	if err != nil {
		log.Fatal(err)
	}

	var duplicates []interface{}

	for cursor.Next(ctx) {
		// decode document
		var dups Duplicates
		err := cursor.Decode(&dups)
		if err != nil {
			log.Fatal(err)
		}
		// keep 1 item from slice
		toRemove := dups.Dups[1:]
		// add all other ids from slice to duplicates array
		for _, v := range toRemove {
			duplicates = append(duplicates, v)
		}
	}
	fmt.Printf("\nDuplicates found: %v\n", len(duplicates))
	// remove duplicates
	if len(duplicates) > 0 {
		res, err := collection.DeleteMany(ctx, bson.D{{"_id", bson.D{{"$in", duplicates}}}})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Duplicates removed: %v\n", res.DeletedCount)
	}
}
