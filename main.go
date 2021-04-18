package main

import (
	"context"
	"encoding/json"
	"flag"
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

	// Use command line flags to avoid running unwanted parts of the program while testing
	var queryOpenSea bool
	var insertRecords bool
	flag.BoolVar(&queryOpenSea, "q", false, "Query the OpenSea API starting at the timestamp in OCCURRED_BEFORE_DATE")
	flag.BoolVar(&insertRecords, "i", false, "Insert new records into MongoDB")

	flag.Parse()

	// OpenSea events API capped at 200 pages at a time
	// Rerunning the task manually after updating timestamp environment variable appears to be enough delay to reset counter
	if queryOpenSea {
		fmt.Println("Querying OpenSea...")
		for i := 0; i <= 200; i++ {
			events := fetchEvents(i, timestamp)

			if events == nil {
				fmt.Println("No events returned")
				break
			}
			if insertRecords {
				_, err := collection.InsertMany(ctx, events)
				if err != nil {
					log.Fatal(err)
				}
			}
			fmt.Print(".")
		}
	}
	removeDuplicates(collection)
	uniqueTokens := getUniqueTokens(collection)
	uniqueIds := fetchCoinGeckoIds(uniqueTokens)
	updateHistoricalTokenPrices(client, uniqueIds)
	getLatestRecord(collection)

	fmt.Println("Program complete")
}

// OpenSeaEvents is an interface for a single OpenSea Event API response
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

// EventDate is an interface for OpenSea event data from Mongo
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

// Duplicates is an interface for duplicates in intermediate bson structure
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

type OpenSeaToken struct {
	Address string `bson:"address"`
	Symbol  string `bson:"_id"`
	Name    string `bson:"name"`
}

type CoinGeckoToken struct {
	Id        string `json:"id"`
	Platforms struct {
		Address string `json:"ethereum"`
	} `json:"platforms"`
}

// Get all unique payment tokens used in events collection
func getUniqueTokens(events *mongo.Collection) []OpenSeaToken {
	ctx := context.TODO()

	// group by token symbol
	groupStage := bson.D{{
		"$group",
		bson.D{
			{"_id", "$payment_token.symbol"},
			{"address", bson.D{{
				"$first", "$payment_token.address"},
			}},
			{"name", bson.M{"$first": "$payment_token.name"}},
		},
	}}
	cursor, err := events.Aggregate(ctx, mongo.Pipeline{groupStage})
	if err != nil {
		log.Fatal(err)
	}
	// aggregate results into slice of tokens
	var tokens []OpenSeaToken
	for cursor.Next(ctx) {
		var addr OpenSeaToken
		err := cursor.Decode(&addr)
		if err != nil {
			return nil
		}
		tokens = append(tokens, addr)
	}

	return tokens
}

type TokenId struct {
	Id     string
	Symbol string
}

// Match tokens from OpenSea data to ids for querying CoinGecko API
func fetchCoinGeckoIds(openSeaTokens []OpenSeaToken) []TokenId {
	// get list of all coins from CoinGecko
	url := "https://api.coingecko.com/api/v3/coins/list?include_platform=true"
	response, err := http.Get(url)
	if err != nil {
		log.Printf("Request Failed: %s", err)
		return nil
	}

	var coinGeckoTokens []CoinGeckoToken
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Printf("Read Failed: %s", err)
		return nil
	}
	err = json.Unmarshal(body, &coinGeckoTokens)

	ids := []TokenId{{"ethereum", "ETH"}}

	for _, token := range coinGeckoTokens {
		// extract matching token addresses
		for _, v := range openSeaTokens {
			if token.Platforms.Address == v.Address && v.Address != "" {
				ids = append(ids, TokenId{token.Id, v.Symbol})
			}
		}
	}
	return ids
}

// Given an array of ids, fetch historical prices for all of them and add them to the collection
// TODO: take timestamps for start/end instead of hard-coding
func updateHistoricalTokenPrices(client *mongo.Client, ids []TokenId) {
	ctx := context.TODO()
	// connect to token collection
	db := os.Getenv("MONGO_DATABASE")
	coll := os.Getenv("MONGO_PRICE_COLLECTION")
	tokensCollection := client.Database(db).Collection(coll)

	// For testing only: drop collection every time until I figure out correct data format
	err := tokensCollection.Drop(ctx)
	if err != nil {
		log.Fatal(err)
	}

	for _, id := range ids {
		// get price history from CoinGecko
		prices := getSingleTokenHistory(id)
		// insert into new collection
		_, err := tokensCollection.InsertMany(ctx, prices)
		if err != nil {
			log.Fatal(err)
		}
	}
}

type Price struct {
	Symbol    string  `json:"symbol"`
	Timestamp int64   `json:"timestamp"`
	Price     float64 `json:"usd_price" bson:"usd_price"`
}

// Prices is a slice of [timestamp, price] historic values for a given token returned by CoinGecko API
type Prices struct {
	Prices [][2]float64 `json:"prices"`
}

// Get list of historic token prices for the given coin ID
func getSingleTokenHistory(id TokenId) []interface{} {
	// hard-coding timestamps to known existing data, should be variables
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/%s/market_chart/range?vs_currency=usd&from=1615744719&to=1616599000",
		id.Id)
	response, err := http.Get(url)
	if err != nil {
		fmt.Printf("Request Failed: %s", err)
		return nil
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Printf("Read Failed: %s", err)
		return nil
	}
	var prices Prices
	err = json.Unmarshal(body, &prices)
	if err != nil {
		fmt.Printf("Unmarshal Failed: %s", err)
		return nil
	}

	// translate results from plain array to Price object with keys
	var returnPrices []interface{}
	for _, v := range prices.Prices {
		returnPrices = append(returnPrices, Price{id.Symbol, int64(v[0]), v[1]})
	}
	return returnPrices
}
