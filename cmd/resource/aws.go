package resource

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"math"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"

	"github.com/ahmetb/go-linq/v3"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/lambda/lambdaiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/aws/aws-sdk-go/service/sts/stsiface"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

type clusterData struct {
	endpoint           string
	CAData             []byte
	resourcesVpcConfig *eks.VpcConfigResponse
}

type S3API s3iface.S3API
type LambdaAPI lambdaiface.LambdaAPI
type STSAPI stsiface.STSAPI
type SecretsManagerAPI secretsmanageriface.SecretsManagerAPI
type EKSAPI eksiface.EKSAPI
type EC2API ec2iface.EC2API

type AWSClients struct {
	AWSSession *session.Session
	AWSClientsIface
}

type AWSClientsIface interface {
	S3Client(region *string, role *string) S3API
	LambdaClient(region *string, role *string) LambdaAPI
	STSClient(region *string, role *string) STSAPI
	SecretsManagerClient(region *string, role *string) SecretsManagerAPI
	EKSClient(region *string, role *string) EKSAPI
	EC2Client(region *string, role *string) EC2API
	Session(region *string, role *string) *session.Session
}

func (c *AWSClients) S3Client(region *string, role *string) S3API {
	return s3.New(c.Session(region, role))
}

func (c *AWSClients) LambdaClient(region *string, role *string) LambdaAPI {
	return lambda.New(c.Session(region, role))
}

func (c *AWSClients) STSClient(region *string, role *string) STSAPI {
	return sts.New(c.Session(region, role))
}

func (c *AWSClients) SecretsManagerClient(region *string, role *string) SecretsManagerAPI {
	return secretsmanager.New(c.Session(region, role))
}

func (c *AWSClients) EKSClient(region *string, role *string) EKSAPI {
	return eks.New(c.Session(region, role))
}

func (c *AWSClients) EC2Client(region *string, role *string) EC2API {
	return ec2.New(c.Session(region, role))
}

func (c *AWSClients) Session(region *string, role *string) *session.Session {
	if region != nil || role != nil {
		return c.AWSSession.Copy(c.Config(region, role))
	}
	return c.AWSSession
}

func (c *AWSClients) Config(region *string, role *string) *aws.Config {
	config := aws.NewConfig().WithMaxRetries(10)

	if region != nil {
		config = config.WithRegion(*region)
	}
	if role != nil {
		creds := stscreds.NewCredentials(c.AWSSession, *role)
		config = config.WithCredentials(creds)
	}
	return config
}

// getClusterDetails use describe_cluster API
func getClusterDetails(svc eksiface.EKSAPI, clusterName string) (*clusterData, error) {
	log.Printf("Getting cluster data...")
	c := &clusterData{}
	input := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}
	result, err := svc.DescribeCluster(input)
	if err != nil {
		return nil, AWSError(err)
	}
	switch *result.Cluster.Status {
	case eks.ClusterStatusActive:
		c.endpoint = *result.Cluster.Endpoint
		c.CAData, err = base64.StdEncoding.DecodeString(*result.Cluster.CertificateAuthority.Data)
		if err != nil {
			return nil, genericError("Decoding CA", err)
		}
		c.resourcesVpcConfig = result.Cluster.ResourcesVpcConfig
	default:
		return nil, fmt.Errorf("cluster %s in unexpected state %s", clusterName, *result.Cluster.Status)
	}
	return c, nil
}

// generateKubeToken using the aws-iam-auth pkg
func generateKubeToken(svc STSAPI, clusterID *string) (*string, error) {
	roleArn, err := getCurrentRoleARN(svc)
	if err != nil {
		return nil, genericError("Could not get token: ", err)
	}
	log.Printf("Generating token for cluster: %s, role: %s", *clusterID, *roleArn)
	gen, err := token.NewGenerator(false, false)
	if err != nil {
		return nil, genericError("Could not get token: ", err)
	}
	tok, err := gen.GetWithSTS(*clusterID, svc)
	if err != nil {
		return nil, genericError("Could not get token: ", err)
	}
	return &tok.Token, nil
}

// downloadS3 download file from S3 to specified path.
func downloadS3(svc S3API, bucket string, key string, filename string) error {
	log.Printf("Getting file from S3...")

	// Create a downloader with the session and default options
	downloader := s3manager.NewDownloaderWithClient(svc)

	// Create a file to write the S3 Object contents to.
	f, err := os.Create(filename)
	if err != nil {
		return genericError("downloadS3", err)
	}

	// Write the contents of S3 Object to the file
	numBytes, err := downloader.Download(f, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return genericError("downloadS3", err)
	}

	log.Printf("Downloaded %s - %v bytes ", f.Name(), numBytes)
	return nil
}

//getSecretsManager and returns bytes data.
func getSecretsManager(svc SecretsManagerAPI, arn *string) ([]byte, error) {
	log.Printf("Getting data from Secrets Manager...")

	input := &secretsmanager.GetSecretValueInput{
		SecretId:     arn,
		VersionStage: aws.String("AWSCURRENT"),
	}
	result, err := svc.GetSecretValue(input)
	if err != nil {
		return nil, AWSError(err)
	}

	// Decrypts secret using the associated KMS CMK.
	// Depending on whether the secret is a string or binary, one of these fields will be populated.
	var secretString []byte
	if result.SecretString != nil {
		secretString = []byte(*result.SecretString)
	} else {
		decodedBinarySecretBytes := make([]byte, base64.StdEncoding.DecodedLen(len(result.SecretBinary)))
		len, err := base64.StdEncoding.Decode(decodedBinarySecretBytes, result.SecretBinary)
		if err != nil {
			return nil, genericError("Base64 Decode Error:", err)
		}
		secretString = []byte(string(decodedBinarySecretBytes[:len]))
	}
	return secretString, nil
}

func getBucketRegion(svc S3API, bucket string) (*string, error) {
	log.Printf("Checking S3 bucket region...")
	ctx := context.Background()
	region, err := s3manager.GetBucketRegionWithClient(ctx, svc, bucket)
	if err != nil {
		return nil, AWSError(err)
	}
	log.Printf("Found S3 bucket region: %v", region)
	return aws.String(region), nil
}

func getCurrentRoleARN(svc STSAPI) (*string, error) {
	input := &sts.GetCallerIdentityInput{}
	response, err := svc.GetCallerIdentity(input)
	if err != nil {
		return nil, AWSError(err)
	}
	return toRoleArn(response.Arn), nil
}

func toRoleArn(arn *string) *string {
	arnParts := strings.Split(*arn, ":")
	if arnParts[2] != "sts" || !strings.HasPrefix(arnParts[5], "assumed-role") {
		return arn
	}
	arnParts = strings.Split(*arn, "/")
	arnParts[0] = strings.Replace(arnParts[0], "assumed-role", "role", 1)
	arnParts[0] = strings.Replace(arnParts[0], ":sts:", ":iam:", 1)
	arn = aws.String(arnParts[0] + "/" + arnParts[1])
	return arn
}

func getVpcConfig(ekssvc EKSAPI, ec2svc EC2API, model *Model) (*VPCConfiguration, error) {
	if model.ClusterID == nil || !IsZero(model.VPCConfiguration) {
		return nil, nil
	}
	resp, err := getClusterDetails(ekssvc, *model.ClusterID)
	if err != nil {
		return nil, err
	}
	if *resp.resourcesVpcConfig.EndpointPublicAccess == true && *resp.resourcesVpcConfig.PublicAccessCidrs[0] == "0.0.0.0/0" {
		return nil, nil
	}
	log.Println("Detected private cluster, adding VPC Configuration...")
	subnets, err := filterSubnetsWithNATorTransitGatewayTargets(ec2svc, resp.resourcesVpcConfig.SubnetIds)
	if err != nil {
		return nil, err
	}
	if IsZero(subnets) {
		return nil, fmt.Errorf("no subnets with NAT/Transit Gateway found for the cluster %s, use VPCConfiguration to specify VPC settings", aws.StringValue(model.ClusterID))
	}
	log.Printf("Using Subnets: %v, SecurityGroups: %v", aws.StringValueSlice(subnets), aws.StringValueSlice(resp.resourcesVpcConfig.SecurityGroupIds))

	return &VPCConfiguration{
		SecurityGroupIds: aws.StringValueSlice(resp.resourcesVpcConfig.SecurityGroupIds),
		SubnetIds:        aws.StringValueSlice(subnets),
	}, nil
}

func filterSubnetsWithNATorTransitGatewayTargets(ec2client ec2iface.EC2API, subnets []*string) (filtered []*string, err error) {
	resp, err := ec2client.DescribeSubnets(&ec2.DescribeSubnetsInput{
		SubnetIds: subnets,
	})
	if err != nil {
		return filtered, err
	}
	for _, subnet := range resp.Subnets {
		resp, err := ec2client.DescribeRouteTables(&ec2.DescribeRouteTablesInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("association.subnet-id"),
					Values: []*string{subnet.SubnetId},
				},
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{subnet.VpcId},
				},
			},
		})
		if err != nil {
			return filtered, err
		}
		if IsZero(resp.RouteTables) {
			resp, err = ec2client.DescribeRouteTables(&ec2.DescribeRouteTablesInput{
				Filters: []*ec2.Filter{
					{
						Name:   aws.String("association.main"),
						Values: []*string{aws.String("true")},
					},
					{
						Name:   aws.String("vpc-id"),
						Values: []*string{subnet.VpcId},
					},
				},
			})
			if err != nil {
				return filtered, err
			}
		}
		for _, route := range resp.RouteTables[0].Routes {
			if route.NatGatewayId != nil || route.TransitGatewayId != nil {
				filtered = append(filtered, subnet.SubnetId)
				break
			}
		}
	}
	if len(filtered) > LambdaMaxSubnets {
		log.Printf("Found more subnets than the Lambda supported limit... Limiting the subnet to %v", LambdaMaxSubnets)
		return getMaxSubnets(ec2client, filtered, LambdaMaxSubnets)
	}
	return filtered, err
}

func getMaxSubnets(ec2client ec2iface.EC2API, subnets []*string, max int) (filtered []*string, err error) {
	resp, err := ec2client.DescribeSubnets(&ec2.DescribeSubnetsInput{
		SubnetIds: subnets,
	})

	if err != nil {
		return filtered, err
	}

	// get uniq azs from the subnets
	azs := linq.From(resp.Subnets).SelectT(func(s *ec2.Subnet) string {
		return aws.StringValue(s.AvailabilityZone)
	}).OrderBy(func(item interface{}) interface{} { return item }).Distinct().Results()

	// get the per AZ from the max count
	var count int
	count = int(math.Round(float64(max / len(azs))))
	if count == 0 {
		count = 1
	}
	for _, a := range azs {
		var subnets []*string
		linq.From(resp.Subnets).WhereT(func(s *ec2.Subnet) bool {
			return aws.StringValue(s.AvailabilityZone) == a
		}).SelectT(func(s *ec2.Subnet) *string {
			return s.SubnetId
		}).Take(count).ToSlice(&subnets)
		filtered = append(filtered, subnets...)
	}
	return filtered, nil
}
