package main

import (
	"context"
	"goclassifieds/lib/ads"
	"goclassifieds/lib/attr"
	"goclassifieds/lib/entity"
	"goclassifieds/lib/vocab"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	session "github.com/aws/aws-sdk-go/aws/session"
	elasticsearch7 "github.com/elastic/go-elasticsearch/v7"
	"github.com/mitchellh/mapstructure"
	"github.com/tangzero/inflector"
)

func handler(ctx context.Context, s3Event events.S3Event) {

	elasticCfg := elasticsearch7.Config{
		Addresses: []string{
			"https://i12sa6lx3y:v75zs8pgyd@classifieds-4537380016.us-east-1.bonsaisearch.net:443",
		},
	}

	esClient, err := elasticsearch7.NewClient(elasticCfg)
	if err != nil {

	}

	sess := session.Must(session.NewSession())

	for _, record := range s3Event.Records {

		pieces := strings.Split(record.S3.Object.Key, "/")

		pluralName := inflector.Pluralize(pieces[0])
		singularName := inflector.Singularize(pieces[0])

		entityManager := entity.NewDefaultManager(entity.DefaultManagerConfig{
			SingularName: singularName,
			PluralName:   pluralName,
			Index:        "classified_" + pluralName,
			EsClient:     esClient,
			Session:      sess,
			UserId:       "",
		})

		id := pieces[1][0 : len(pieces[1])-8]
		ent := entityManager.Load(id, "default")

		if singularName == "ad" {
			ent = IndexAd(ent)
		}

		entityManager.Save(ent, "elastic")
	}
}

func IndexAd(obj map[string]interface{}) map[string]interface{} {

	var item ads.Ad
	mapstructure.Decode(obj, &item)

	allAttrValues := make([]attr.AttributeValue, 0)
	for _, attrValue := range item.Attributes {
		attributesFlattened := attr.FlattenAttributeValue(attrValue)
		for _, flatAttr := range attributesFlattened {
			attr.FinalizeAttributeValue(&flatAttr)
			allAttrValues = append(allAttrValues, flatAttr)
		}
	}
	item.Attributes = allAttrValues

	for index, featureSet := range item.FeatureSets {
		allFeatureTerms := make([]vocab.Term, 0)
		for _, term := range featureSet.Terms {
			flatTerms := vocab.FlattenTerm(term, true)
			for _, flatTerm := range flatTerms {
				allFeatureTerms = append(allFeatureTerms, flatTerm)
			}
		}
		item.FeatureSets[index].Terms = allFeatureTerms
	}

	ent, _ := ads.ToEntity(&item)
	return ent

}

func main() {
	lambda.Start(handler)
}
