package util

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const (
	ManagedByTagKey   = "managed-by"
	ManagedByTagValue = "terraform-provider-rawtree"
)

func ManagedByTagMap() map[string]string {
	return map[string]string{
		ManagedByTagKey: ManagedByTagValue,
	}
}

func ManagedByIAMTag() iamtypes.Tag {
	return iamtypes.Tag{
		Key:   aws.String(ManagedByTagKey),
		Value: aws.String(ManagedByTagValue),
	}
}
