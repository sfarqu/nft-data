# NFT Data Collector

A quick and dirty tool to grab sale event data from an NFT marketplace API and put it in a database for easier data
visualization.

Requires a MongoDB instance set up at a URI you have API access to.

## Usage

### Set up environment variables
Rename `.env.example` to `.env`.
Replace the values in `.env` to match your configured MongoDB instance and preferred values.

| Variable | Description |
| -------- | ----------- |
| MONGO_URI | URI of your MongoDB instance |
| MONGO_DATABASE | MongoDB database name |
| MONGO_COLLECTION | MongoDB collection name in which to insert OpenSea events |
| MONGO_PRICE_COLLECTION | MongoDB collection name in which to insert historic USD prices of payment tokens |
| OCCURRED_BEFORE_DATE | Unix timestamp of the latest event to query from OpenSea API |

### Running script
`go run main.go -q -i`

This script will make up to 200 API requests of 50 transactions each to the OpenSea API "events" endpoint, and insert 
the responses into MongoDB. This is the maximum number of scripted requests permitted by the API.

In order to collect more than 10,000 transactions, the API query contains a timestamp to indicate which records to return.
The script can be run multiple times by editing the `OCCURRED_BEFORE_DATE` environment variable. For convenience, the script
prints out the timestamp of the earliest record saved to the database, for easy pasting into the environment variable.

The OpenSea events include the type of token that was used to pay for the NFT, but the `usd_price` for each token is 
from the time that you run the script, *not* the time that the event happened. In order to approximate the value at time
of sale, historic prices for each token are pulled from the CoinGecko API and inserted into a different collection. A 
join on the two collections is performed in order to calculate an approximate sale price.

### Using MongoDB aggregation pipelines
This script does not calculate sale prices in USD, it simply inserts the API results into MongoDB. For this project, 
calculating sale price and generating charts was done using MongoDB aggregation pipelines. The contents of those 
pipelines have been included in this repository under the `mongo` directory for transparency.