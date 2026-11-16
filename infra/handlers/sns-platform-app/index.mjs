// Custom resource: an SNS mobile-push platform application whose credential
// lives in Secrets Manager. CloudFormation has no native resource for SNS
// platform applications, and a credential passed as a resource property
// would sit in plaintext in the template. Instead the template carries only
// the secret's ARN; this handler reads the value at deploy time.
//
// Properties: Name, Platform (e.g. "GCM"), CredentialSecretArn,
//             Rotation (opaque; bump it to re-read the secret and push the
//             new credential to SNS).
// For GCM, a service-account JSON credential makes SNS use FCM HTTP v1
// token auth (the read-only AuthenticationMethod attribute reports "Token";
// it cannot be set).
// Ref / PhysicalResourceId: the platform application ARN.
//
// Never log the event's secret value — only the ARN ever appears in props.
import {
  SNSClient,
  CreatePlatformApplicationCommand,
  SetPlatformApplicationAttributesCommand,
  DeletePlatformApplicationCommand,
} from '@aws-sdk/client-sns';
import {
  SecretsManagerClient,
  GetSecretValueCommand,
} from '@aws-sdk/client-secrets-manager';

const sns = new SNSClient({});
const sm = new SecretsManagerClient({});

export const handler = async (event) => {
  const p = event.ResourceProperties;
  console.log(JSON.stringify({
    requestType: event.RequestType,
    physicalResourceId: event.PhysicalResourceId,
    name: p.Name,
    platform: p.Platform,
  }));

  if (event.RequestType === 'Delete') {
    const arn = event.PhysicalResourceId;
    if (arn && arn.startsWith('arn:')) {
      await sns.send(new DeletePlatformApplicationCommand({ PlatformApplicationArn: arn }));
    }
    return { PhysicalResourceId: arn };
  }

  const secret = await sm.send(new GetSecretValueCommand({ SecretId: p.CredentialSecretArn }));
  const attributes = { PlatformCredential: secret.SecretString };

  const old = event.OldResourceProperties;
  if (event.RequestType === 'Update' && old.Name === p.Name && old.Platform === p.Platform) {
    // Same app: rotate the credential in place, keep the ARN (and with it
    // every device endpoint registered under it).
    await sns.send(new SetPlatformApplicationAttributesCommand({
      PlatformApplicationArn: event.PhysicalResourceId,
      Attributes: attributes,
    }));
    return { PhysicalResourceId: event.PhysicalResourceId };
  }

  // Create, or a rename/platform change (a replacement: CloudFormation then
  // sends Delete for the old ARN during cleanup).
  const res = await sns.send(new CreatePlatformApplicationCommand({
    Name: p.Name,
    Platform: p.Platform,
    Attributes: attributes,
  }));
  return { PhysicalResourceId: res.PlatformApplicationArn };
};
