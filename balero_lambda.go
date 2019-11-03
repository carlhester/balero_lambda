package main

import "fmt"
import "context"
import "net/http"
import "io/ioutil"
import "strconv"
import "strings"
import "sort"
import "time"
import "encoding/json"
import "github.com/aws/aws-sdk-go/aws"
import "github.com/aws/aws-sdk-go/aws/session"
import "github.com/aws/aws-sdk-go/service/sns"
import "github.com/aws/aws-lambda-go/lambda"
import "github.com/aws/aws-lambda-go/events"

func main() {
	lambda.Start(HandleRequest)
}

func HandleRequest(ctx context.Context, snsEvent events.SNSEvent) {
	const KEY = "MW9S-E7SL-26DU-VV8V" // public use key from bart website

	for _, record := range snsEvent.Records {
		snsRecord := record.SNS
		message := SNSMessage{}
		_ = json.Unmarshal([]byte(snsRecord.Message), &message)

		if !(strings.EqualFold(message.MessageBody, "ready")) {
			return
		}

		var phone string = message.OriginationNumber
		var station string = "MONT"
		var dir string = "n"
		var targetLine string = "YELLOW"
		var timeWindow int = 15
		var dirText string = "North"

		if dir == "s" {
			dirText = "South"
		}

		ackTxt := fmt.Sprintf("Hi! Here are the next three %s line trains heading %s from %s within %d minutes of each other.\n", strings.ToLower(targetLine), strings.ToLower(dirText), strings.ToLower(station), timeWindow)
		SendSNS(ackTxt, phone)

		url := "http://api.bart.gov/api/etd.aspx?cmd=etd&orig=" + station + "&key=" + KEY + "&dir=" + dir + "&json=y"
		resp, err := http.Get(url)
		if err != nil {
			panic(err.Error())
		}
		data, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		if err != nil {
			panic(err.Error())
		}

		usableData := RawDataIntoDataStruct(data)

		var targetTrains []string
		var targetMinutes []string

		for _, train := range usableData.Root.Station[0].Etd {
			for _, est := range train.Est {
				if strings.EqualFold(est.Color, targetLine) {
					targetTrains = append(targetTrains, train.Abbreviation)
					targetMinutes = append(targetMinutes, est.Minutes)
				}
			}
		}

		loc, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			panic(err.Error())
		}
		currTime := time.Now()
		currTime = currTime.In(loc)
		timeStamp := fmt.Sprintf("%s", currTime.Format("Jan _2 15:04:05"))

		intMin := convertStrMinutesToInt(targetMinutes)
		sort.Ints(intMin)

		alertMsg := timeStamp
		numResults := 0

		if len(intMin) > 2 {
			for i, _ := range intMin[:len(intMin)-2] {
				twoTrainDelta := intMin[i+2] - intMin[i]
				if twoTrainDelta <= timeWindow {
					partAlertMsg := fmt.Sprintf("%s %d \n%s %d \n%s %d\n%d", targetTrains[i], intMin[i], targetTrains[i+1], intMin[i+1], targetTrains[i+2], intMin[i+2], twoTrainDelta)
					alertMsg = fmt.Sprintf("%s\n%s\n", alertMsg, partAlertMsg)
					numResults += 1
				}
			}
		}
		if numResults > 0 {
			SendSNS(alertMsg, phone)
		} else {
			SendSNS("No trains found", phone)
		}

	}
	return
}

func convertStrMinutesToInt(minutes []string) []int {
	var intMin []int
	for _, strMin := range minutes {
		if strMin == "Leaving" {
			strMin = "0"
		}
		i, err := strconv.Atoi(strMin)
		if err != nil {
			panic(err.Error())
		}
		intMin = append(intMin, i)
	}
	return intMin
}

func SendSNS(message string, phone string) {
	sess := session.Must(session.NewSession())
	svc := sns.New(sess)

	params := &sns.PublishInput{
		Message:     aws.String(message),
		PhoneNumber: aws.String(phone),
	}
	_, err := svc.Publish(params)

	if err != nil {
		fmt.Println(err.Error())
		return
	}
}

func RawDataIntoDataStruct(rawData []byte) *Data {
	var usableData Data
	json.Unmarshal([]byte(rawData), &usableData)
	return &usableData
}

type Estimates []struct {
	Minutes     string `json:"minutes"`
	Direction   string `json:"direction"`
	Length      int    `json:"length"`
	Color       string `json:"color"`
	Hexcolor    string `json:"hexcolor"`
	Bikeflag    int    `json:"bikeflag"`
	Delay       int    `json:"delay"`
	Carflag     int    `json:"carflag"`
	Cancelflag  int    `json:"cancelflag"`
	Dynamicflag int    `json:"dynamicflag"`
}

type Etd []struct {
	Destination  string    `json:"destination"`
	Abbreviation string    `json:"abbreviation"`
	Limited      int       `json:"limited"`
	Est          Estimates `json:"estimate"`
}

type Station []struct {
	Name string `json:"name"`
	Abbr string `json:"abbr"`
	Etd  Etd    `json:"etd"`
}

type Uri struct {
	Cdata string `json:"#cdata-section"`
}

type Root struct {
	Id      int     `json:"@id"`
	Uri     Uri     `json:"uri"`
	Date    string  `json:"date"`
	Time    string  `json:"time"`
	Station Station `json:"station"`
	Message string  `json:"message"`
}

type Xml struct {
	Version  string `json:"@version"`
	Encoding string `json:"@encoding"`
}

type Data struct {
	Xml  Xml  `json:"?xml"`
	Root Root `json:"root"`
}

type SNSMessage struct {
	OriginationNumber          string `json:"originationNumber"`
	DestinationNumber          string `json:"DestinationNumber"`
	MessageKeyword             string `json:"messageKeyword"`
	MessageBody                string `json:"messageBody"`
	InboundMessageId           string `json:"inboundMessageId"`
	PreviousPublishedMessageId string `json:"previousPublishedMessageId"`
}
