package main

import "fmt"
import "context"
import "net/http"
import "io/ioutil"
import "os"
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
import "github.com/aws/aws-sdk-go/service/dynamodb"
import "github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

func main() {
	lambda.Start(HandleRequest)
}

func HandleRequest(ctx context.Context, snsEvent events.SNSEvent) {
	const KEY = "MW9S-E7SL-26DU-VV8V" // public use key from bart website

	for _, record := range snsEvent.Records {
		snsRecord := record.SNS
		message := SNSMessage{}
		_ = json.Unmarshal([]byte(snsRecord.Message), &message)

		c := getContact(message.OriginationNumber)

		if len(c.Phone) == 0 {
			c.Phone = message.OriginationNumber
			updateContact(c)
			txtMsg := fmt.Sprintf("New user. Added %s to db", c.Phone)
			SendSMS(txtMsg, c)
			return
		}

		msg := strings.ToLower(message.Body)

		switch msg {
		case "12th", "16th", "19th", "24th", "ashb", "antc", "balb", "bayf", "cast", "civc", "cols", "colm", "conc", "daly", "dbrk", "dubl", "deln", "plza", "embr", "frmt", "ftvl", "glen", "hayw", "lafy", "lake", "mcar", "mlbr", "mont", "nbrk", "ncon", "oakl", "orin", "pitt", "pctr", "phil", "powl", "rich", "rock", "sbrn", "sfia", "sanl", "shay", "ssan", "ucty", "warm", "wcrk", "wdub", "woak":
			c.Station = msg
			updateContact(c)

		case "n", "s":
			c.Dir = msg
			updateContact(c)

		case "yellow", "red", "blue", "orange", "green":
			c.Line = msg
			updateContact(c)

		}

		var timeWindow int = 15

		if msg == "whoami" {
			provideUserConfig(c)
			return
		}

		if !(strings.EqualFold(message.Body, "ready")) {
			return
		}

		ackTxt := fmt.Sprintf("Hi! Here are the next three %s line trains heading %s from %s within %d minutes of each other.\n",
			strings.ToLower(c.Line), strings.ToLower(c.Dir), strings.ToLower(c.Station), timeWindow)
		SendSMS(ackTxt, c)

		url := prepareUrl(c.Station, KEY, c.Dir)
		rawData := rawDataFromUrl(url)
		usableData := RawDataIntoDataStruct(rawData)

		var targetTrains []string
		var targetMinutes []string

		for _, train := range usableData.Root.Station[0].Etd {
			for _, est := range train.Est {
				if strings.EqualFold(est.Color, c.Line) {
					targetTrains = append(targetTrains, train.Abbreviation)
					targetMinutes = append(targetMinutes, est.Minutes)
				}
			}
		}

		timeStamp := getTimestamp()

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
			SendSMS(alertMsg, c)
		} else {
			SendSMS("No trains found", c)
		}

	}
	return
}

func getTimestamp() string {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err.Error())
	}
	currTime := time.Now()
	currTime = currTime.In(loc)
	timeStamp := fmt.Sprintf("%s", currTime.Format("Jan _2 15:04:05"))
	return timeStamp
}

func setupNewUser(contact Contact) {
	SendSMS("Welcome!", contact)
	updateContact(contact)
}

func provideUserConfig(c Contact) {
	contact := getContact(c.Phone)
	result := fmt.Sprintf("%s - %s - %s", contact.Dir, contact.Station, contact.Line)
	SendSMS(result, contact)
}

func rawDataFromUrl(url string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		panic(err.Error())
	}
	data, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()

	if err != nil {
		panic(err.Error())
	}
	return data
}

func prepareUrl(station string, key string, dir string) string {
	url := "http://api.bart.gov/api/etd.aspx?cmd=etd&orig=" + station + "&key=" + key + "&dir=" + dir + "&json=y"
	return url
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

func SendSMS(message string, contact Contact) {
	sess := session.Must(session.NewSession())
	svc := sns.New(sess)

	params := &sns.PublishInput{
		Message:     aws.String(message),
		PhoneNumber: aws.String(contact.Phone),
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

func isNewContact(c Contact) bool {
	contact := getContact(c.Phone)
	if len(contact.Phone) > 0 {
		return false
	}
	return true
}

func getContact(ph string) Contact {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := dynamodb.New(sess)
	result, err := svc.GetItem(&dynamodb.GetItemInput{
		TableName: aws.String("db_test"),
		Key: map[string]*dynamodb.AttributeValue{
			"Phone": {
				S: aws.String(ph),
			},
		},
	})

	if err != nil {
		fmt.Println(err.Error())
	}

	contact := Contact{}
	err = dynamodbattribute.UnmarshalMap(result.Item, &contact)
	if err != nil {
		fmt.Errorf("failed to unmarshal Query result items, %v", err)
	}

	return contact
}

func updateContact(c Contact) {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	svc := dynamodb.New(sess)

	updContact := Contact{
		Phone:   c.Phone,
		Dir:     c.Dir,
		Station: c.Station,
		Line:    c.Line,
	}

	av, err := dynamodbattribute.MarshalMap(updContact)

	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	input := &dynamodb.PutItemInput{
		Item:      av,
		TableName: aws.String("db_test"),
	}

	_, err = svc.PutItem(input)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

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
	Body                       string `json:"messageBody"`
	InboundMessageId           string `json:"inboundMessageId"`
	PreviousPublishedMessageId string `json:"previousPublishedMessageId"`
}

type Contact struct {
	Phone   string
	Dir     string
	Station string
	Line    string
}
