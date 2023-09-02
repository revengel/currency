package main

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"golang.org/x/text/encoding/charmap"
)

const (
	urlTemplate       = "https://www.cbr.ru/scripts/XML_daily.asp?date_req=%s"
	urlDateTimeFormat = "2006-01-02T15:04:05"
	outputDateFormat  = "02.01.2006"
	xmlDateFormat     = "02/01/2006"

	usdCurrency = "usd"
	eurCurrency = "eur"
	uahCurrency = "uah"

	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36"
)

var (
	httpClient = http.Client{
		Timeout: time.Second * 2, // Timeout after 2 seconds
	}
	cachePath      = filepath.Join(os.Getenv("HOME"), ".cache", "currency", "cache")
	cacheStorage   *bolt.DB
	currenciesRate = map[string][]string{}
)

type Valute struct {
	XMLName  xml.Name `xml:"Valute"`
	ID       string   `xml:"ID,attr"`
	NumCode  int64    `xml:"NumCode"`
	CharCode string   `xml:"CharCode"`
	Nominal  int64    `xml:"Nominal"`
	Name     string   `xml:"Name"`
	Value    string   `xml:"Value"`
	Date     time.Time
}

type ValCurs struct {
	XMLName xml.Name  `xml:"ValCurs"`
	Date    string    `xml:"Date,attr"`
	Name    string    `xml:"name,attr"`
	Valutes []*Valute `xml:"Valute"`
}

func (v Valute) getRow() (row []string, err error) {
	valStr := strings.Replace(v.Value, ",", ".", -1)
	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return
	}

	divOn := float64(v.Nominal)

	return []string{
		v.Date.Format(outputDateFormat),
		strings.ToUpper(v.CharCode),
		fmt.Sprintf("%.2f", val/divOn),
	}, err
}

func getCurrencyRates(t time.Time) (out map[string][]string, err error) {
	if len(currenciesRate) > 0 {
		return currenciesRate, nil
	}

	var url = fmt.Sprintf(urlTemplate, t.Format(xmlDateFormat))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}

	req.Header.Set("User-Agent", userAgent)

	res, err := httpClient.Do(req)
	if err != nil {
		return
	}

	if res.Body == nil {
		return out, errors.New("Response body are empty")
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("status code error: %s", res.Status)
		return
	}

	var v ValCurs
	d := xml.NewDecoder(res.Body)
	d.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		switch charset {
		case "windows-1251":
			return charmap.Windows1251.NewDecoder().Reader(input), nil
		default:
			return nil, fmt.Errorf("unknown charset: %s", charset)
		}
	}
	err = d.Decode(&v)
	if err != nil {
		return
	}

	for _, val := range v.Valutes {
		val.Date = t
		row, err := val.getRow()
		if err != nil {
			return out, err
		}
		currenciesRate[strings.ToLower(val.CharCode)] = row
	}

	return currenciesRate, nil
}

func getCurrencyRate(name string, t time.Time) (out []string, err error) {
	vals, err := getCurrencyRates(t)
	if err != nil {
		return
	}

	if val, ok := vals[strings.ToLower(name)]; ok {
		return val, nil
	}

	err = fmt.Errorf("cannot get currency rate for '%s'", name)
	return
}

func getCurrencyItemCache(name string, t time.Time, skipCache bool) (r []string, err error) {
	var cacheKey = fmt.Sprintf("%s-%s", t.Format(outputDateFormat), name)
	var val []byte
	var tx *bolt.Tx

	tx, err = cacheStorage.Begin(true)
	if err != nil {
		return
	}

	defer tx.Rollback()

	var b *bolt.Bucket
	b, err = tx.CreateBucketIfNotExists([]byte("cache"))
	if err != nil {
		return
	}

	if skipCache {
		goto skipCache
	}

	val = b.Get([]byte(cacheKey))
	if val == nil {
		goto skipCache
	}

	err = json.Unmarshal(val, &r)
	if err != nil {
		return
	}

	return

skipCache:
	r, err = getCurrencyRate(name, t)
	if err != nil {
		return
	}

	val, err = json.Marshal(r)
	if err != nil {
		return
	}

	err = b.Put([]byte(cacheKey), val)
	if err != nil {
		return
	}

	err = tx.Commit()
	if err != nil {
		return
	}

	return
}

func main() {
	var (
		currency   = flag.String("currency", usdCurrency, "currency code")
		skipCache  = flag.Bool("skip-cache", false, "skip cache")
		daysBefore = flag.Int("days-before", 0, "get currency rate in date x days before")
		rows       [][]string
		err        error
	)
	flag.Parse()

	err = os.MkdirAll(filepath.Dir(cachePath), 0777)
	if err != nil {
		log.Fatal(err)
	}

	cacheStorage, err = bolt.Open(cachePath, 0600, nil)
	if err != nil {
		log.Fatal(err)
	}

	defer cacheStorage.Close()

	currenciesList := strings.Split(*currency, ",")
	if len(currenciesList) == 0 {
		log.Fatal("select at least one currency")
	}

	var date = time.Now().Add(time.Duration(-*daysBefore) * 24 * time.Hour)
	for _, curr := range currenciesList {
		row, err := getCurrencyItemCache(curr, date, *skipCache)
		if err != nil {
			log.Fatal(err)
		}
		rows = append(rows, row)
	}

	var writer = csv.NewWriter(os.Stdout)
	writer.Comma = '\t'
	err = writer.WriteAll(rows)
	if err != nil {
		log.Fatal(err)
	}
}
