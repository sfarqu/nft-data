# NFT Data Collector

A quick and dirty tool to grab sale event data from an NFT marketplace API and put it in a database for easier data
visualization.

Requires a [MongoDB](https://www.mongodb.com/) instance set up at a URI you have API access to.

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

This script will make up to 200 API requests of 50 transactions each to the 
[OpenSea API](https://docs.opensea.io/reference#retrieving-asset-events) "events" endpoint, and insert 
the responses into MongoDB. This is the maximum number of scripted requests permitted by the API without an API key.

In order to collect more than 10,000 transactions, the API query contains a timestamp to indicate which records to return.
The script can be run multiple times by editing the `OCCURRED_BEFORE_DATE` environment variable. For convenience, the script
prints out the timestamp of the earliest record saved to the database, for easy pasting into the environment variable.

The API request is filtered to "successful" events, which indicate completed sales. Including other event types proved
to take up too much database space for not much value.

This script also pulls historic prices for all payment tokens found in the OpenSea transactions from a different API,
CoinGecko.

#### Command Line options
Some of the script functionality requires command-line flags to trigger. This is simply because I didn't want to run queries
or insert into the database over and over when testing later functionality

Flag | Description
---- | :----------
q | **query**: If this flag is enabled, the script will query the OpenSea API 200 times
i | **insert**: If this flag is enabled, queried OpenSea records will be inserted into the database. Requires `-q`

## MongoDB aggregation pipelines
This script does not calculate sale prices in USD, it simply inserts the API results into MongoDB. For this project, 
calculating sale price and generating charts was done using MongoDB aggregation pipelines. The contents of those 
pipelines have been included in this repository under the `mongo` directory for transparency.

Using MongoDB is outside the scope of this document, but these pipelines can be pasted into the MongoDB interface to
test

#### 1. Truncate Historic Timestamps to Hour
The [CoinGecko API](https://www.coingecko.com/en/api) returns historical market data at different granularity depending 
on the selected time range. This repo uses a single API call to get history for a 10-day span, which returns hourly prices.
Unfortunately timestamps were not *exactly* on the hour, and since fuzzy-matching dates in a join is a pain, timestamps
were truncated to the nearest hour. The same process could be done for the per-minute API data, it just would have taken
more API calls, for not that much difference in prices.

This aggregation runs on the `MONGO_PRICE_COLLECTION` collection and outputs to a new collection.

#### 2. Combine Adjusted Token Prices
This pipeline is run on the `MONGO_COLLECTION` collection. It does a lookup to the historic prices collection to get a 
roughly-accurate historic USD price for each transaction. This calculated field, `usd_historic_price`, is used by the 
following aggregations.

This pipeline outputs to a new collection because it kept timing out on our dataset. This doubles storage requirements, 
but made making charts easier.

#### 3. Calculate Total Price
In the OpenSea data, total sale price is stored as the number of payment tokens spent, but as text and without decimal 
places. Details about the payment token are also stored, including the number of decimal places that should be inserted 
into the total to get the actual number of tokens, and the price of the token in US dollars.

The formula used to get an approximate dollar value for each sale is:
* Convert numbers stored as strings to actual numbers
* Raise 10 to the power of `decimals` to get a denominator to turn `total_price` into a number of tokens
* Divide `total_price` by denominator
* Multiply number of tokens by USD price of token at time of sale

Unfortunately the `usd_price` for each token is from the time that you run the script, *not* the time that the event 
happened. In order to approximate the value at time of sale, historic prices for each token are pulled from the CoinGecko 
API and used instead of the `usd_price` value from OpenSea.

This aggregation is used by charts referencing primary sale price, and is integrated in the Median By Collection pipeline.

#### 4. Median By Collection
This was the first pipeline we tried, and it is probably not the most efficient way to calculate the median. Pipeline is
roughly based off [this article](https://www.compose.com/articles/mongo-metrics-finding-a-happy-median/), but the article
uses an incorrect median calculation, which has been corrected.

Grouping by collection was done partially because trying to run this aggregation on the entire dataset kept timing out.
It also provides more interesting data points for graphing.