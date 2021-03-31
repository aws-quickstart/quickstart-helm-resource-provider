# AWSQS::Kubernetes::Helm

This repository provides AWS CloudFormation options for managing Helm 3 resources in EKS and self-managed Kubernetes clusters. For properties and available attributes (ReadOnlyProperties), see 
the [schema](./awsqs-kubernetes-helm.json).

## Installation
```bash
aws cloudformation create-stack \
  --stack-name awsqs-kubernetes-helm-resource \
  --capabilities CAPABILITY_NAMED_IAM \
  --template-url https://s3.amazonaws.com/aws-quickstart/quickstart-helm-resource-provider/deploy.template.yaml \
  --region us-west-2

aws cloudformation describe-stacks \
--stack-name awsqs-kubernetes-helm-resource | jq -r ".Stacks[0].Outputs[0].OutputValue" 
```
To help you deploy resources into your account, use the provided [template](./deploy.template.yaml). Use an ARN role to provide access to the cluster. For self-managed Kubernetes clusters, upload the **kubeconfig** file to AWS Secrets Manager.

## Example usage

```yaml
AWSTemplateFormatVersion: 2010-09-09
Parameters:
  Cluster:
    Type: String
Resources:
  TestResource:
    Type: AWSQS::Kubernetes::Helm
    Properties:
      Chart: stable/jenkins
      ClusterID: !Ref Cluster
      Values:
        master.serviceType: LoadBalancer
Outputs:
  Name:
    Value: !GetAtt TestResource.Name
```