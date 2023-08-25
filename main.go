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
	"strings"
	"time"
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

func getCurrencyItem(name string) (r currencyData, err error) {
	info, ok := currenciesInfo[name]
	if !ok {
		err = fmt.Errorf("invalid currency name: %s", name)
		return
	}

	var url = fmt.Sprintf(urlTemplate, info.code, time.Now().Unix())
	res, err := httpClient.Get(url)
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

func main() {
	var (
		currencies = flag.String("currencies", usdCurrency, "currency codes (comma separated)")
		usd        = flag.Bool(usdCurrency, false, "show usd info")
		eur        = flag.Bool(eurCurrency, false, "show eur info")
		uah        = flag.Bool(uahCurrency, false, "show uah info")
		rows       [][]string
		err        error
	)
	flag.Parse()

	currenciesList := strings.Split(*currencies, ",")
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
		r, err := getCurrencyItem(currency)
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
