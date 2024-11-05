package utils

import (
	"context"
	"fmt"
	oadpv1alpha1 "github.com/openshift/oadp-operator/api/v1alpha1"
	"github.com/openshift/oadp-operator/pkg/credentials"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func GetProviderSecret(secretName string, secretNamespace string, k8sClient client.Client, ctx context.Context) (corev1.Secret, error) {

	secret := corev1.Secret{}
	key := types.NamespacedName{
		Name:      secretName,
		Namespace: secretNamespace,
	}
	err := k8sClient.Get(ctx, key, &secret)

	if err != nil {
		return secret, err
	}
	originalSecret := secret.DeepCopy()
	// replace carriage return with new line
	secret.Data = ReplaceCarriageReturn(secret.Data)
	k8sClient.Patch(ctx, &secret, client.MergeFrom(originalSecret))
	return secret, nil
}

func ParseAWSSecret(secret corev1.Secret, secretKey string, matchProfile string) (string, string, error) {

	AWSAccessKey, AWSSecretKey, profile := "", "", ""
	splitString := strings.Split(string(secret.Data[secretKey]), "\n")
	keyNameRegex, err := regexp.Compile(`\[.*\]`)
	const (
		accessKeyKey = "aws_access_key_id"
		secretKeyKey = "aws_secret_access_key"
	)
	if err != nil {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: keyNameRegex")
	}
	awsAccessKeyRegex, err := regexp.Compile(`\b` + accessKeyKey + `\b`)
	if err != nil {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: awsAccessKeyRegex")
	}
	awsSecretKeyRegex, err := regexp.Compile(`\b` + secretKeyKey + `\b`)
	if err != nil {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: awsSecretKeyRegex")
	}
	for index, line := range splitString {
		if line == "" {
			continue
		}
		if keyNameRegex.MatchString(line) {
			awsProfileRegex, err := regexp.Compile(`\[|\]`)
			if err != nil {
				return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: keyNameRegex")
			}
			cleanedLine := strings.ReplaceAll(line, " ", "")
			parsedProfile := awsProfileRegex.ReplaceAllString(cleanedLine, "")
			if parsedProfile == matchProfile {
				profile = matchProfile
				// check for end of arr
				if index+1 >= len(splitString) {
					break
				}
				for _, profLine := range splitString[index+1:] {
					if profLine == "" {
						continue
					}
					matchedAccessKey := awsAccessKeyRegex.MatchString(profLine)
					matchedSecretKey := awsSecretKeyRegex.MatchString(profLine)

					if err != nil {
						return AWSAccessKey, AWSSecretKey, err
					}
					if matchedAccessKey { // check for access key
						AWSAccessKey, err = GetMatchedKeyValue(accessKeyKey, profLine)
						if err != nil {
							return AWSAccessKey, AWSSecretKey, err
						}
						continue
					} else if matchedSecretKey { // check for secret key
						AWSSecretKey, err = GetMatchedKeyValue(secretKeyKey, profLine)
						if err != nil {
							return AWSAccessKey, AWSSecretKey, err
						}
						continue
					} else {
						break // aws credentials file is only allowed to have profile followed by aws_access_key_id, aws_secret_access_key
					}
				}
			}
		}
	}
	if profile == "" {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("error finding AWS Profile for the supplied AWS credential")
	}
	if AWSAccessKey == "" {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("error finding access key id for the supplied AWS credential")
	}
	if AWSSecretKey == "" {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("error finding secret access key for the supplied AWS credential")
	}

	return AWSAccessKey, AWSSecretKey, nil
}

// Return value to the right of = sign with quotations and spaces removed.
func GetMatchedKeyValue(key string, s string) (string, error) {
	for _, removeChar := range []string{"\"", "'", " "} {
		s = strings.ReplaceAll(s, removeChar, "")
	}
	for _, prefix := range []string{key, "="} {
		s = strings.TrimPrefix(s, prefix)
	}
	if len(s) == 0 {
		return s, fmt.Errorf("secret parsing error in key " + key)
	}
	return s, nil
}

func GetSecretNameAndKey(config map[string]string, credential *corev1.SecretKeySelector, secretNamespace string, plugin oadpv1alpha1.DefaultPlugin, k8sClient client.Client, ctx context.Context) (string, string, error) {
	// Assume default values unless user has overriden them
	secretName := credentials.PluginSpecificFields[plugin].SecretName
	secretKey := credentials.PluginSpecificFields[plugin].PluginSecretKey
	if _, ok := config["credentialsFile"]; ok {
		if secretName, secretKey, err :=
			credentials.GetSecretNameKeyFromCredentialsFileConfigString(config["credentialsFile"]); err == nil {
			return secretName, secretKey, nil
		}
	}
	// check if user specified the Credential Name and Key
	if credential != nil {
		if len(credential.Name) > 0 {
			secretName = credential.Name
		}
		if len(credential.Key) > 0 {
			secretKey = credential.Key
		}
	}

	err := VerifySecretContent(secretName, secretNamespace, secretKey, k8sClient, ctx)
	if err != nil {
		return secretName, secretKey, err
	}

	return secretName, secretKey, nil
}

func VerifySecretContent(secretName string, secretNamespace string, secretKey string, k8sClient client.Client, ctx context.Context) error {
	secret, err := GetProviderSecret(secretName, secretNamespace, k8sClient, ctx)
	if err != nil {
		return err
	}
	data, foundKey := secret.Data[secretKey]
	if !foundKey || len(data) == 0 {
		return fmt.Errorf("Secret name %s is missing data for key %s", secretName, secretKey)
	}
	return nil
}

func ReplaceCarriageReturn(data map[string][]byte) map[string][]byte {
	for k, v := range data {
		// report if carriage return is found
		if strings.Contains(string(v), "\r\n") {
			data[k] = []byte(strings.ReplaceAll(string(v), "\r\n", "\n"))
		}
	}
	return data
}
