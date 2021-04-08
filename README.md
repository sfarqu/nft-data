# NFT Data Collector

A quick and dirty tool to grab sale event data from an NFT marketplace API and put it in a database for easier data
visualization.

Requires a MongoDB instance set up at a URI you have API access to.

## Usage

### Set up environment variables
Rename `.env.example` to `.env`.
Replace the URI, database name, and collection name in `.env` to match your configured MongoDB instance.
Set `OCCURRED_BEFORE_DATE` to a Unix timestamp representing the end of the period you want to query.

### Running script
`go run main.go`

This script will make up to 200 API requests of 50 transactions each to the OpenSea API "events" endpoint, and insert the responses into Mongo. 
This is the maximum number of scripted requests permitted by the API.

In order to collect more than 10,000 transactions, the API query contains a timestamp to indicate which records to return.
The script can be run multiple times by editing the `OCCURRED_BEFORE_DATE` environment variable. For convenience, the script
prints out the timestamp of the earliest record saved to the database, for easy pasting into the environment variable.