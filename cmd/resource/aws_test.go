package resource

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
)

// Define mock structs.
type mockEKSClient struct {
	EKSAPI
}

type mockEC2Client struct {
	EC2API
}

type mockSecretsManagerClient struct {
	SecretsManagerAPI
}

type mockSTSClient struct {
	STSAPI
}

type mockS3Client struct {
	S3API
}

func (m *mockAWSClients) EKSClient(region *string, role *string) EKSAPI {
	return &mockEKSClient{}
}
func (m *mockAWSClients) EC2Client(region *string, role *string) EC2API {
	return &mockEC2Client{}
}
func (m *mockAWSClients) S3Client(region *string, role *string) S3API {
	return &mockS3Client{}
}
func (m *mockAWSClients) STSClient(region *string, role *string) STSAPI {
	return &mockSTSClient{}
}
func (m *mockAWSClients) LambdaClient(region *string, role *string) LambdaAPI {
	return &mockLambdaClient{}
}
func (m *mockAWSClients) SecretsManagerClient(region *string, role *string) SecretsManagerAPI {
	return &mockSecretsManagerClient{}
}
func (m *mockAWSClients) Session(region *string, role *string) *session.Session {
	return MockSession
}

func (m *mockEKSClient) DescribeCluster(c *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
	clusters := map[string]struct {
		data *eks.Cluster
	}{
		"eks": {
			data: &eks.Cluster{
				Arn: aws.String("arn:aws:eks:us-east-2:1234567890:cluster/eks"),
				CertificateAuthority: &eks.Certificate{
					Data: aws.String("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0="),
				},
				Endpoint: aws.String("https://EKS.yl4.us-east-2.eks.amazonaws.com"),
				Name:     aws.String("eks"),
				Status:   aws.String(eks.ClusterStatusActive),
				ResourcesVpcConfig: &eks.VpcConfigResponse{
					EndpointPublicAccess: aws.Bool(true),
					PublicAccessCidrs:    aws.StringSlice([]string{"0.0.0.0/0"}),
				},
			},
		},
		"private": {
			data: &eks.Cluster{
				Arn: aws.String("arn:aws:eks:us-east-2:1234567890:cluster/private"),
				CertificateAuthority: &eks.Certificate{
					Data: aws.String("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0="),
				},
				Endpoint: aws.String("https://private.yl4.us-east-2.eks.amazonaws.com"),
				Name:     aws.String("eks"),
				Status:   aws.String(eks.ClusterStatusActive),
				ResourcesVpcConfig: &eks.VpcConfigResponse{
					EndpointPublicAccess:  aws.Bool(false),
					PublicAccessCidrs:     aws.StringSlice([]string{"0.0.0.0/0"}),
					EndpointPrivateAccess: aws.Bool(true),
					SecurityGroupIds:      aws.StringSlice([]string{"sg-01"}),
					SubnetIds:             aws.StringSlice([]string{"subnet-01", "subnet-02"}),
				},
			},
		},
		"private-nonat": {
			data: &eks.Cluster{
				Arn: aws.String("arn:aws:eks:us-east-2:1234567890:cluster/private"),
				CertificateAuthority: &eks.Certificate{
					Data: aws.String("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0="),
				},
				Endpoint: aws.String("https://private.yl4.us-east-2.eks.amazonaws.com"),
				Name:     aws.String("eks"),
				Status:   aws.String(eks.ClusterStatusActive),
				ResourcesVpcConfig: &eks.VpcConfigResponse{
					EndpointPublicAccess:  aws.Bool(false),
					PublicAccessCidrs:     aws.StringSlice([]string{"0.0.0.0/0"}),
					EndpointPrivateAccess: aws.Bool(true),
					SecurityGroupIds:      aws.StringSlice([]string{"sg-01"}),
					SubnetIds:             aws.StringSlice([]string{"subnet-01"}),
				},
			},
		},
		"eks1": {
			data: &eks.Cluster{
				Arn:    aws.String("arn:aws:eks:us-east-2:1234567890:cluster/eks1"),
				Name:   aws.String("eks1"),
				Status: aws.String(eks.ClusterStatusCreating),
			},
		},
		"eks2": {
			data: &eks.Cluster{
				Arn: aws.String("arn:aws:eks:us-east-2:1234567890:cluster/eks2"),
				CertificateAuthority: &eks.Certificate{
					Data: aws.String("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0="),
				},
				Endpoint: aws.String("https://EKS2.yl4.us-east-2.eks.amazonaws.com"),
				Name:     aws.String("eks2"),
				Status:   aws.String(eks.ClusterStatusUpdating),
			},
		},
	}
	for name, d := range clusters {
		if aws.StringValue(c.Name) == name {
			return &eks.DescribeClusterOutput{
				Cluster: d.data,
			}, nil
		}
	}
	return &eks.DescribeClusterOutput{
		Cluster: nil,
	}, fmt.Errorf("%s", eks.ErrCodeNotFoundException)
}

func (m *mockEC2Client) DescribeSubnets(i *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
	subnets := []*ec2.Subnet{}
	for _, subnet := range i.SubnetIds {
		subnets = append(subnets, &ec2.Subnet{SubnetId: subnet, VpcId: aws.String("vpc-01"), AvailabilityZone: aws.String(fmt.Sprintf("az-%v", rand.Intn(3-1+1)+1))})
	}
	return &ec2.DescribeSubnetsOutput{
		Subnets: subnets,
	}, nil
}

func (m *mockEC2Client) DescribeRouteTables(i *ec2.DescribeRouteTablesInput) (*ec2.DescribeRouteTablesOutput, error) {
	d := map[string]*ec2.RouteTable{
		"subnet-01": {
			Routes: []*ec2.Route{
				{DestinationCidrBlock: aws.String("10.0.0.0/16"), GatewayId: aws.String("local")},
				{DestinationCidrBlock: aws.String("0.0.0.0/0"), GatewayId: aws.String("igw-01")},
			},
		},
		"subnet-02": {
			Routes: []*ec2.Route{
				{DestinationCidrBlock: aws.String("10.0.0.0/16"), GatewayId: aws.String("local")},
				{DestinationCidrBlock: aws.String("0.0.0.0/0"), NatGatewayId: aws.String("nat-01")},
			},
		},
		"subnet-03": {
			Routes: []*ec2.Route{
				{DestinationCidrBlock: aws.String("10.0.0.0/16"), GatewayId: aws.String("local")},
				{DestinationCidrBlock: aws.String("0.0.0.0/0"), TransitGatewayId: aws.String("tgw-01")},
			},
		},
		"subnet-04": {
			Routes: []*ec2.Route{
				{DestinationCidrBlock: aws.String("10.0.0.0/16"), GatewayId: aws.String("local")},
				{DestinationCidrBlock: aws.String("1.2.3.4/16"), TransitGatewayId: aws.String("tgw-01")},
				{DestinationCidrBlock: aws.String("0.0.0.0/0"), TransitGatewayId: aws.String("tgw-02")},
			},
		},
		"vpc-01": {
			Routes: []*ec2.Route{
				{DestinationCidrBlock: aws.String("10.0.0.0/16"), GatewayId: aws.String("local")},
				{DestinationCidrBlock: aws.String("0.0.0.0/0"), NatGatewayId: aws.String("nat-01")},
			},
		},
	}
	var s string
	for _, filter := range i.Filters {
		if aws.StringValue(filter.Name) == "association.main" {
			s = "vpc-01"
			break
		}
		if aws.StringValue(filter.Name) == "association.subnet-id" {
			s = aws.StringValue(filter.Values[0])
			if s == "subnet-05" {
				return &ec2.DescribeRouteTablesOutput{RouteTables: []*ec2.RouteTable{}}, nil
			}

		}
	}

	return &ec2.DescribeRouteTablesOutput{RouteTables: []*ec2.RouteTable{d[s]}}, nil
}

func (m *mockSecretsManagerClient) GetSecretValue(s *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
	secrets := map[string]struct {
		GetSecretValueOutput *secretsmanager.GetSecretValueOutput
	}{
		"sec1": {
			GetSecretValueOutput: &secretsmanager.GetSecretValueOutput{
				ARN:          aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt"),
				Name:         aws.String("kubeconfig"),
				SecretString: aws.String("Test"),
			},
		},
		"sec2": {
			GetSecretValueOutput: &secretsmanager.GetSecretValueOutput{
				ARN:          aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wtttt"),
				Name:         aws.String("kubeconfig1"),
				SecretBinary: []byte("Test"),
			},
		},
	}
	for _, d := range secrets {
		if aws.StringValue(s.SecretId) == aws.StringValue(d.GetSecretValueOutput.ARN) {
			return d.GetSecretValueOutput, nil
		}
	}
	return nil, fmt.Errorf("Notfound err")
}

func (m *mockSTSClient) GetCallerIdentity(*sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {

	return &sts.GetCallerIdentityOutput{
		Account: aws.String("1234567890"),
		Arn:     aws.String("arn:aws:sts::1234567890:assumed-role/TestRole/session-1587810408"),
		UserId:  aws.String("AROA4OQMRFUSJUBK2CBCZ:session-1587810408"),
	}, nil
}

func (m *mockSTSClient) GetCallerIdentityRequest(*sts.GetCallerIdentityInput) (req *request.Request, output *sts.GetCallerIdentityOutput) {
	op := &request.Operation{
		Name:       "GetCallerIdentity",
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}
	input := &sts.GetCallerIdentityInput{}
	output = &sts.GetCallerIdentityOutput{
		Account: aws.String("1234567890"),
		Arn:     aws.String("arn:aws:sts::1234567890:assumed-role/TestRole/session-1587810408"),
		UserId:  aws.String("AROA4OQMRFUSJUBK2CBCZ:session-1587810408"),
	}

	req = awsRequest(op, input, output)
	return
}

func (m *mockS3Client) HeadBucketRequest(input *s3.HeadBucketInput) (req *request.Request, output *s3.HeadBucketOutput) {
	op := &request.Operation{
		Name:       "HeadObject",
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}
	if input == nil {
		input = &s3.HeadBucketInput{}
	}

	output = &s3.HeadBucketOutput{}

	req = awsRequest(op, input, output)
	return
}

func (m *mockS3Client) GetObjectWithContext(ctx aws.Context, input *s3.GetObjectInput, opts ...request.Option) (*s3.GetObjectOutput, error) {
	data, _ := ioutil.ReadFile(TestFolder + "/test.yaml")
	return &s3.GetObjectOutput{
		Body:          ioutil.NopCloser(bytes.NewReader(data[:])),
		ContentLength: aws.Int64(int64(len(data))),
	}, nil
}

func testSetupGetBucketRegionServer(region string, statusCode int, incHeader bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if incHeader {
			w.Header().Set("X-Amz-Bucket-Region", region)
		}
		w.WriteHeader(statusCode)
	}))
}

func dlLoggingSvcNoChunk(data []byte) (*s3.S3, *[]string) {
	var m sync.Mutex
	names := []string{}

	svc := s3.New(MockSession)
	svc.Handlers.Send.Clear()
	svc.Handlers.Send.PushBack(func(r *request.Request) {
		m.Lock()
		defer m.Unlock()

		names = append(names, r.Operation.Name)

		r.HTTPResponse = &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewReader(data[:])),
			Header:     http.Header{},
		}
		r.HTTPResponse.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))
	})

	return svc, &names
}

func TestGetClusterDetails(t *testing.T) {
	// Setup Test
	mockSvc := &mockEKSClient{}
	clusters := []string{"eks", "eks1", "eks2"}

	for _, cluster := range clusters {
		t.Run(cluster, func(t *testing.T) {
			_, err := getClusterDetails(mockSvc, cluster)
			if err != nil {
				assert.Contains(t, err.Error(), "in unexpected state")
			}
		})
	}
}

func TestGenerateKubeToken(t *testing.T) {
	mockSvc := &mockSTSClient{}
	cluster := aws.String("eks")
	_, err := generateKubeToken(mockSvc, cluster)
	assert.Nil(t, err)
}

func TestGetSecretsManager(t *testing.T) {
	// Setup Test
	mockSvc := &mockSecretsManagerClient{}
	secrets := []string{"arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt", "arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wtttt", "arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig"}
	expectedErr := "Notfound err"
	//expectedRes := []byte("Test")
	for _, sec := range secrets {
		t.Run(sec, func(t *testing.T) {
			_, err := getSecretsManager(mockSvc, &sec)
			if err != nil {
				assert.Contains(t, err.Error(), expectedErr)
			}
			//assert.Equal(t, res, expectedRes)
		})
	}
}

func TestDownloadS3(t *testing.T) {
	testFile := "/tmp/test"
	defer os.Remove(testFile)
	data, _ := ioutil.ReadFile(TestZipFile)
	s, _ := dlLoggingSvcNoChunk(data)
	tests := []string{"Success", "BadRequest"}
	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			if test == "BadRequest" {
				s.Handlers.Send.PushBack(func(r *request.Request) {
					r.HTTPResponse.StatusCode = 400
					r.HTTPResponse.Body = ioutil.NopCloser(bytes.NewReader([]byte{}))
				})
			}
			err := downloadS3(s, "bucket", "key", testFile)
			if err != nil {
				assert.Contains(t, err.Error(), test)
			}
		})
	}
}

func TestGetBucketRegion(t *testing.T) {
	sess := MockSession
	expectedErr := "NotFound"
	tests := map[string]struct {
		bucket   string
		eRegion  string
		respone  bool
		httpCode int
	}{
		"Correct": {
			bucket:   "test-bucket",
			eRegion:  "us-east-2",
			respone:  true,
			httpCode: 200,
		},
		"NonExt": {
			bucket:   "no-bucket",
			respone:  false,
			httpCode: 404,
		},
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			server := testSetupGetBucketRegionServer(d.eRegion, d.httpCode, d.respone)
			defer server.Close()
			svc := s3.New(sess.Copy(), &aws.Config{
				Region:     aws.String("hint-region"),
				Endpoint:   aws.String(server.URL),
				DisableSSL: aws.Bool(true),
			})

			result, err := getBucketRegion(svc, d.bucket)
			if err != nil {
				assert.Contains(t, err.Error(), expectedErr)
			}
			assert.EqualValues(t, d.eRegion, aws.StringValue(result))
		})
	}
}

func TestGetCurrentRoleARN(t *testing.T) {
	// Setup Test
	mockSvc := &mockSTSClient{}
	expectedARN := aws.String("arn:aws:iam::1234567890:role/TestRole")
	expectedErr := "SomeError"
	res, err := getCurrentRoleARN(mockSvc)
	if err != nil {
		assert.Contains(t, err.Error(), expectedErr)
	}
	assert.EqualValues(t, aws.StringValue(expectedARN), aws.StringValue(res))
}

func TestToRoleArn(t *testing.T) {
	arns := []string{"arn:aws:sts::1234567890:assumed-role/TestRole/session-1587810408", "arn:aws:iam::1234567890:role/TestRole"}
	expectedARN := aws.String("arn:aws:iam::1234567890:role/TestRole")
	for _, arn := range arns {
		t.Run(arn, func(t *testing.T) {
			res := toRoleArn(aws.String(arn))
			assert.EqualValues(t, aws.StringValue(expectedARN), aws.StringValue(res))
		})
	}
}

func TestGetVpcConfig(t *testing.T) {
	tests := map[string]struct {
		m *Model
	}{
		"Public": {
			m: &Model{
				ClusterID: aws.String("eks"),
			},
		},
		"Private": {
			m: &Model{
				ClusterID: aws.String("private"),
			},
		},
		"PrivateWithoutNatGW": {
			m: &Model{
				ClusterID: aws.String("private-nonat"),
			},
		},
	}
	eErr := "no subnets with NAT/Transit Gateway found"
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			//d.m.VPCConfiguration = nil
			_, err := getVpcConfig(&mockEKSClient{}, &mockEC2Client{}, d.m)
			if err != nil {
				assert.Contains(t, err.Error(), eErr)
			}

		})
	}
}

func TestFilterSubnetsWithNATorTransitGatewayTargets(t *testing.T) {
	mockSvc := &mockEC2Client{}
	tests := map[string]struct {
		subnets  []*string
		eSubnets []*string
	}{
		"NATSubnets": {
			subnets:  []*string{aws.String("subnet-01"), aws.String("subnet-02"), aws.String("subnet-05")},
			eSubnets: []*string{aws.String("subnet-02"), aws.String("subnet-05")},
		},
		"TransitGatewaySubnets": {
			subnets:  []*string{aws.String("subnet-01"), aws.String("subnet-03"), aws.String("subnet-04")},
			eSubnets: []*string{aws.String("subnet-03"), aws.String("subnet-04")},
		},
		"NoSubnets": {
			subnets: []*string{aws.String("subnet-01")},
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := filterSubnetsWithNATorTransitGatewayTargets(mockSvc, d.subnets)
			assert.Nil(t, err)
			assert.ElementsMatch(t, d.eSubnets, result)
		})
	}
}

func TestGetMaxSubnets(t *testing.T) {
	mockSvc := &mockEC2Client{}
	tests := map[string]struct {
		subnets       []*string
		max           int
		eSubnetsCount int
	}{
		"AzMoreThanMax": {
			subnets:       []*string{aws.String("subnet-01"), aws.String("subnet-02"), aws.String("subnet-03")},
			max:           2,
			eSubnetsCount: 2,
		},
		"MoreThanMax": {
			subnets:       []*string{aws.String("subnet-01"), aws.String("subnet-02"), aws.String("subnet-03"), aws.String("subnet-04"), aws.String("subnet-05"), aws.String("subnet-06"), aws.String("subnet-07"), aws.String("subnet-08"), aws.String("subnet-09"), aws.String("subnet-10"), aws.String("subnet-11"), aws.String("subnet-12"), aws.String("subnet-13"), aws.String("subnet-14"), aws.String("subnet-15"), aws.String("subnet-16"), aws.String("subnet-17"), aws.String("subnet-18"), aws.String("subnet-19"), aws.String("subnet-20")},
			max:           16,
			eSubnetsCount: 2,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := getMaxSubnets(mockSvc, d.subnets, d.max)
			assert.Nil(t, err)
			assert.LessOrEqual(t, d.eSubnetsCount, len(result))
		})
	}
}
