// Copyright © 2018 Ed Silva <ed@edlitmus.info>.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/leekchan/accounting"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/resty.v1"
)

var cfgFile string
var ticker string
var shares int64
var sharesSold int64
var strikePrice float64
var startTime string
var endTime string
var vestStart time.Time
var vestEnd time.Time

type JsonQuote struct {
	GlobalQuote struct {
		Symbol           string `json:"01. symbol"`
		Open             string `json:"02. open"`
		High             string `json:"03. high"`
		Low              string `json:"04. low"`
		Price            string `json:"05. price"`
		Volume           string `json:"06. volume"`
		LatestTradingDay string `json:"07. latest trading day"`
		PreviousClose    string `json:"08. previous close"`
		Change           string `json:"09. change"`
		ChangePercent    string `json:"10. change percent"`
	} `json:"Global Quote"`
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "worth",
	Short: "Find out the value of your stock.",
	Long: `Find out the value of your stock, and figure out how much
longer you have to wait until you're fully vested.
Originally written in perl by Jamie Zawinski.`,
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		vestStart, err = time.Parse(time.RFC3339, viper.GetString("vest-start"))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		vestEnd, err = time.Parse(time.RFC3339, viper.GetString("vest-end"))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		quote, err := getQuote()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		val, err := strconv.ParseFloat(quote.GlobalQuote.Price, 64)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		formatOutput(cmd, val)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getQuote() (JsonQuote, error) {
	var quote JsonQuote
	var err error
	// resty.SetDebug(true)
	resp, err := resty.R().
		SetQueryParams(map[string]string{
			"function": "GLOBAL_QUOTE",
			"symbol":   viper.GetString("ticker"),
			"apikey":   viper.GetString("apikey"),
		}).
		SetHeader("X-Requested-With", "Curl").
		Get("https://www.alphavantage.co/query")
	if err != nil {
		return quote, err
	}
	// resty.SetDebug(false)
	jsn := resp.Body()
	err = json.Unmarshal(jsn, &quote)

	return quote, err
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/worth/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&ticker, "ticker", "", "ticker symbol")
	rootCmd.PersistentFlags().Float64Var(&strikePrice, "strike-price", 0.0, "strike price")
	rootCmd.PersistentFlags().Int64Var(&shares, "shares", 1, "number of shares")
	rootCmd.PersistentFlags().Int64Var(&sharesSold, "shares sold", 0, "number of shares sold")
	rootCmd.PersistentFlags().StringVar(&startTime, "vest-start", "", "vesting start date (RFC3339)")
	rootCmd.PersistentFlags().StringVar(&endTime, "vest-end", "", "vesting end date (RFC3339)")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		configPath := fmt.Sprintf("%s/.config/worth/", home)
		cfgFile = fmt.Sprintf("%sconfig.yaml", configPath)
		dir := filepath.Clean(configPath)
		err = os.MkdirAll(dir, 0700)
		if err != nil {
			log.Fatalf("error creating config file path: %s", err)
		}
		_, err = os.OpenFile(dir+"/config.yaml", os.O_RDONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Fatalf("Error creating config file: %s", err)
		}

		// set config in "~/.config/worth/config.yaml".
		viper.AddConfigPath(configPath)
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		log.Fatalf("Fatal error config file: %s", err)
	}
}

func formatOutput(cmd *cobra.Command, price float64) {
	now := time.Now()
	portionDone := float64(now.Unix()-vestStart.Unix()) / float64(vestEnd.Unix()-vestStart.Unix())

	shares := viper.GetInt64("shares")
	sharesVested := float64(shares) * portionDone
	sharesUnvested := float64(shares) - sharesVested
	sharesVestedAndUnsold := sharesVested - float64(sharesSold)

	ac := accounting.Accounting{Symbol: "$", Precision: 2}

	// subtract the strike price to get the take away value for your shares...
	value := price - viper.GetFloat64("strike-price")
	shareValue := float64(shares) * value

	fmt.Printf("Today's %s price is %s; ", viper.GetString("ticker"), ac.FormatMoney(price))
	fmt.Printf("your total unsold shares are worth %s.\n", ac.FormatMoney(shareValue))

	if portionDone >= 1.0 {
		fmt.Printf("You are 100%% vested.  Why are you still here?\n\n")
		os.Exit(0)
	}

	diff := vestEnd.Sub(now)
	secsToGo := roundTime(diff.Seconds())
	fmt.Printf("You are %d%% vested, for a total of ", int64(portionDone*100))
	fmt.Printf("%d vested unsold shares (%s)\n", int64(sharesVestedAndUnsold), ac.FormatMoney(sharesVestedAndUnsold*value))
	fmt.Printf("But if you quit today, you will walk away from %s\n", ac.FormatMoney(sharesUnvested*value))
	fmt.Printf("Hang in there, little trooper! Only")
	fmt.Printf("%s to go!\n", printSecs(secsToGo))
}

func roundTime(input float64) int64 {
	var result float64

	if input < 0 {
		result = math.Ceil(input - 0.5)
	} else {
		result = math.Floor(input + 0.5)
	}

	// only interested in integer, ignore fractional
	i, _ := math.Modf(result)

	return int64(i)
}

func printSecs(secsToGo int64) string {
	var buffer bytes.Buffer
	var err error

	daysPerYear := 365
	daysPerMonth := (daysPerYear / 12)
	minToGo := int(secsToGo / 60)
	hoursToGo := int(minToGo / 60)
	daysToGo := int(hoursToGo / 24)
	yearsToGo := int(daysToGo / daysPerYear)
	monthsToGo := int(daysToGo / daysPerMonth)

	monthsToGo = monthsToGo - (yearsToGo * 12)
	daysToGo = daysToGo - (yearsToGo * daysPerYear)
	if monthsToGo < 0 {
		monthsToGo = 0
	}

	daysToGo = daysToGo - int(monthsToGo*daysPerMonth)
	if daysToGo < 0 {
		daysToGo = 0
	}

	// kludge to avoid "1 month 30 days", which, while correct, sucks.
	if daysToGo > 29 {
		daysToGo = daysToGo - 30
		monthsToGo = monthsToGo + 1
		if monthsToGo >= 12 {
			monthsToGo = monthsToGo - 12
			yearsToGo = yearsToGo + 1
		}
	}

	if yearsToGo > 0 {
		_, err = buffer.WriteString(fmt.Sprintf(" %d year", yearsToGo))
		if err != nil {
			log.Fatalf("Error: %s", err)
		}
		if yearsToGo != 1 {
			_, err = buffer.WriteString(fmt.Sprint("s"))
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		}
	}

	if monthsToGo > 0 {
		_, err = buffer.WriteString(fmt.Sprintf(" %d month", monthsToGo))
		if err != nil {
			log.Fatalf("Error: %s", err)
		}
		if monthsToGo != 1 {
			_, err = buffer.WriteString(fmt.Sprintf("s"))
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		}
	}

	if daysToGo > 0 {
		_, err = buffer.WriteString(fmt.Sprintf(" %d day", daysToGo))
		if err != nil {
			log.Fatalf("Error: %s", err)
		}
		if daysToGo != 1 {
			_, err = buffer.WriteString(fmt.Sprint("s"))
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		}
	}

	return buffer.String()
}
