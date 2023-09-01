package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	urlTemplate       = "https://www.cbr.ru/Queries/AjaxDataSource/112805?DT=&val_id=%s&_=%d"
	urlDateTimeFormat = "2006-01-02T15:04:05"
	outputDateFormat  = "02.01.2006"

	usdCurrency = "usd"
	eurCurrency = "eur"
	uahCurrency = "uah"
)

var (
	httpClient = http.Client{
		Timeout: time.Second * 2, // Timeout after 2 seconds
	}
	currenciesInfo = map[string]currencyInfo{
		"usd": {
			code: "R01235",
			div:  1,
		},
		"eur": {
			code: "R01239",
			div:  1,
		},
		"uah": {
			code: "R01720",
			div:  10,
		},
	}
	cachePath    = filepath.Join(os.Getenv("HOME"), ".cache", "currency", "cache")
	cacheStorage *bolt.DB
)

type currencyInfo struct {
	code string
	div  float64
}

type currencyData struct {
	Name  string
	DivOn float64
	Date  string  `json:"data"`
	Curs  float64 `json:"curs"`
	Diff  float64 `json:"diff"`
}

func (cr currencyData) getDate() string {
	t, err := time.Parse(urlDateTimeFormat, cr.Date)
	if err != nil {
		panic(err)
	}
	return t.Format(outputDateFormat)
}

func (cr currencyData) getRow() []string {
	return []string{
		cr.getDate(),
		strings.ToUpper(cr.Name),
		fmt.Sprintf("%.2f", cr.Curs/cr.DivOn),
		fmt.Sprintf("%.2f", cr.Diff/cr.DivOn),
	}
}

func getCurrencyItem(name string, t time.Time) (r currencyData, err error) {
	info, ok := currenciesInfo[name]
	if !ok {
		err = fmt.Errorf("invalid currency name: %s", name)
		return
	}

	var url = fmt.Sprintf(urlTemplate, info.code, t.Unix())
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36")

	res, err := httpClient.Do(req)
	if err != nil {
		return r, err
	}

	if res.Body == nil {
		return r, errors.New("Response body are empty")
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("status code error: %s", res.Status)
		return
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return
	}

	data := []currencyData{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		return
	}

	d := data[len(data)-1]
	d.Name = name
	d.DivOn = info.div
	return d, nil
}

func getCurrencyItemCache(name string, t time.Time, skipCache bool) (r currencyData, err error) {
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
	// <-time.After(5 * time.Second)
	r, err = getCurrencyItem(name, t)
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
		currency  = flag.String("currency", usdCurrency, "currency code")
		skipCache = flag.Bool("skip-cache", false, "skip cache")
		usd       = flag.Bool(usdCurrency, false, "show usd info")
		eur       = flag.Bool(eurCurrency, false, "show eur info")
		uah       = flag.Bool(uahCurrency, false, "show uah info")
		rows      [][]string
		err       error
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
	if *usd {
		currenciesList = append(currenciesList, usdCurrency)
	}

	if *eur {
		currenciesList = append(currenciesList, eurCurrency)
	}

	if *uah {
		currenciesList = append(currenciesList, uahCurrency)
	}

	if len(currenciesList) == 0 {
		currenciesList = []string{usdCurrency}
	}

	for _, currency := range currenciesList {
		r, err := getCurrencyItemCache(currency, time.Now(), *skipCache)
		if err != nil {
			log.Fatal(err)
		}
		rows = append(rows, r.getRow())
	}

	var writer = csv.NewWriter(os.Stdout)
	writer.Comma = '\t'
	err = writer.WriteAll(rows)
	if err != nil {
		log.Fatal(err)
	}
}
