package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io/ioutil"
	"log"
	"net/http"
)

func main() {
	ctx := context.TODO()
	uri := "mongodb+srv://cluster0.mt5ze.mongodb.net/nft?authSource=%24external&authMechanism=MONGODB-X509&retryWrites=true&w=majority&tlsCertificateKeyFile=X509-cert-60635618939709594.pem"
	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil { log.Fatal(err) }
	defer client.Disconnect(ctx)
	collection := client.Database("nft").Collection("opensea_events")

	for i:=0; i<200; i++ {
		events := fetchEvents(i)

		if events == nil {
			fmt.Println("No events returned")
			break
		}
		_, err := collection.InsertMany(ctx, events)
		if err != nil { log.Fatal(err) }
		fmt.Print(".")
	}
	getLatestRecord(collection)

	fmt.Println("Program complete")
}

type Events struct {
	Events []interface{} `json:"asset_events"`
}

func fetchEvents(page int) []interface{} {
	url := fmt.Sprintf("https://api.opensea.io/api/v1/events?only_opensea=false&offset=%d&limit=50&occurred_before=1616107702&event_type=successful", page * 50)
	response, err := http.Get(url)
	if err != nil {
		log.Printf("Request Failed: %s", err)
		return nil
	}
	var events Events
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Printf("Read Failed: %s", err)
		return nil
	}
	err = json.Unmarshal(body, &events)

	return events.Events
}

type EventDate struct {
	CreatedDate string `bson:"created_date"`
}

func getLatestRecord(collection *mongo.Collection) {

	opts := options.FindOne().SetSort(bson.D{{"created_date",1}})
	var createdDate EventDate
	err := collection.FindOne(nil, bson.D{}, opts).Decode(&createdDate)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(createdDate.CreatedDate)
}