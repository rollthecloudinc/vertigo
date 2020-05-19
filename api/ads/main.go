package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	ads "goclassifieds/lib/ads"
	es "goclassifieds/lib/es"
	utils "goclassifieds/lib/utils"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	session "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
	elasticsearch7 "github.com/elastic/go-elasticsearch/v7"
	"github.com/gin-gonic/gin"
)

var ginLambda *ginadapter.GinLambda

type AdsController struct {
	EsClient *elasticsearch7.Client
	Session  *session.Session
}

func (c *AdsController) GetAdListItems(context *gin.Context) {
	var req ads.AdListitemsRequest
	if err := context.ShouldBind(&req); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	query := buildAdsSearchQuery(&req)
	ads := es.ExecuteSearch(c.EsClient, &query, "classified_ads")
	for _, ad := range ads {
		log.Printf(" * ID=%s, %s", ad.(map[string]interface{})["_id"], ad.(map[string]interface{})["_source"])
	}
	context.JSON(200, ads)
}

func (c *AdsController) CreateAd(context *gin.Context) {
	var ad ads.Ad
	if err := context.ShouldBind(&ad); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ad.Id = utils.GenerateId()
	ad.Status = ads.Submitted // @todo: Enums not being validated :(
	ad.UserId = utils.GetSubject(context)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(ad); err != nil {
		log.Fatalf("Error encoding query: %s", err)
	}
	StoreDocument(c.Session, &buf, "classifieds-ui-dev", "ads/"+ad.Id+".json.gz")
	context.JSON(200, ad)
}

func StoreDocument(sess *session.Session, body *bytes.Buffer, bucket string, key string) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(body.Bytes()); err != nil {
		log.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		log.Fatal(err)
	}
	uploader := s3manager.NewUploader(sess)
	_, err := uploader.Upload(&s3manager.UploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(key),
		Body:            &buf,
		ContentType:     aws.String("application/json"),
		ContentEncoding: aws.String("gzip"),
	})
	if err != nil {
		log.Fatal(err)
	}
}

func buildAdsSearchQuery(req *ads.AdListitemsRequest) map[string]interface{} {
	filterMust := []interface{}{
		map[string]interface{}{
			"term": map[string]interface{}{
				"adType": map[string]interface{}{
					"value": req.AdType,
				},
			},
		},
	}

	if req.Location != "" {
		cords := strings.Split(req.Location, ",")
		lat, e := strconv.ParseFloat(cords[1], 64)
		if e != nil {

		}
		lon, e := strconv.ParseFloat(cords[0], 64)
		if e != nil {

		}
		geoFilter := map[string]interface{}{
			"geo_distance": map[string]interface{}{
				"validation_method": "ignore_malformed",
				"distance":          "10m",
				"distance_type":     "arc",
				"location": map[string]interface{}{
					"lat": lat,
					"lon": lon,
				},
			},
		}
		filterMust = append(filterMust, geoFilter)
	}

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"filter": []interface{}{
					map[string]interface{}{
						"bool": map[string]interface{}{
							"must": filterMust,
						},
					},
				},
			},
		},
	}

	if req.SearchString != "" || req.Features != nil {

		var matchMust []interface{}

		if req.SearchString != "" {
			matchSearchString := map[string]interface{}{
				"match": map[string]interface{}{
					"title": map[string]interface{}{
						"query": req.SearchString,
					},
				},
			}
			matchMust = append(matchMust, matchSearchString)
		}

		if req.Features != nil {
			matchMust = buildAdFeaturesSearchQuery(matchMust, req.Features)
		}

		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"] = matchMust

	}
	return query
}

func buildAdFeaturesSearchQuery(query []interface{}, features []string) []interface{} {
	for _, feature := range features {
		featureFilter := map[string]interface{}{
			"nested": map[string]interface{}{
				"path": "features",
				"query": map[string]interface{}{
					"bool": map[string]interface{}{
						"must": map[string]interface{}{
							"match": map[string]interface{}{
								"features.humanName": map[string]interface{}{
									"query": feature,
								},
							},
						},
					},
				},
			},
		}
		query = append(query, featureFilter)
	}
	return query
}

func init() {
	// stdout and stderr are sent to AWS CloudWatch Logs
	log.Printf("Gin cold start")

	elasticCfg := elasticsearch7.Config{
		Addresses: []string{
			"https://i12sa6lx3y:v75zs8pgyd@classifieds-4537380016.us-east-1.bonsaisearch.net:443",
		},
	}

	esClient, err := elasticsearch7.NewClient(elasticCfg)
	if err != nil {

	}

	sess := session.Must(session.NewSession())

	adsController := AdsController{EsClient: esClient, Session: sess}

	r := gin.Default()
	r.GET("/ads/adlistitems", adsController.GetAdListItems)
	r.POST("/ads/ad", adsController.CreateAd)

	ginLambda = ginadapter.New(r)
}

func Handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// If no name is provided in the HTTP request body, throw an error
	return ginLambda.ProxyWithContext(ctx, req)
}

func main() {
	lambda.Start(Handler)
}
