import * as path from 'path';
import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as secretsmanager from 'aws-cdk-lib/aws-secretsmanager';
import * as cr from 'aws-cdk-lib/custom-resources';

export interface SnsPlatformAppProps {
  /** Platform application name (unique per account+region+platform). */
  readonly name: string;
  /** SNS platform, e.g. 'GCM' (FCM) or 'APNS'. */
  readonly platform: string;
  /**
   * Secret whose SecretString is the platform credential (for FCM: the
   * Google service-account JSON). Its value is set out of band and never
   * appears in the template; only the ARN does.
   */
  readonly credentialSecret: secretsmanager.ISecret;
  /**
   * Opaque value; change it to make the next deploy re-read the secret and
   * push the credential to SNS (rotation). The ARN does not change.
   */
  readonly rotation?: string;
}

/**
 * An SNS mobile-push platform application owned by CloudFormation through a
 * custom resource (CFN has no native type for it). The credential is read
 * from Secrets Manager at deploy time by the handler Lambda.
 */
export class SnsPlatformApp extends Construct {
  /** The platform application ARN. */
  public readonly arn: string;

  constructor(scope: Construct, id: string, props: SnsPlatformAppProps) {
    super(scope, id);
    const stack = cdk.Stack.of(this);

    const onEvent = new lambda.Function(this, 'Handler', {
      runtime: lambda.Runtime.NODEJS_22_X,
      architecture: lambda.Architecture.ARM_64,
      handler: 'index.handler',
      code: lambda.Code.fromAsset(path.join(__dirname, '../handlers/sns-platform-app')),
      timeout: cdk.Duration.seconds(30),
      description: 'CFN custom resource: SNS platform application with credential from Secrets Manager',
    });
    props.credentialSecret.grantRead(onEvent);
    onEvent.addToRolePolicy(
      new iam.PolicyStatement({
        actions: ['sns:CreatePlatformApplication'],
        resources: ['*'],
      })
    );
    onEvent.addToRolePolicy(
      new iam.PolicyStatement({
        actions: ['sns:SetPlatformApplicationAttributes', 'sns:DeletePlatformApplication'],
        resources: [`arn:${stack.partition}:sns:${stack.region}:${stack.account}:app/${props.platform}/*`],
      })
    );

    const provider = new cr.Provider(this, 'Provider', {
      onEventHandler: onEvent,
    });

    const resource = new cdk.CustomResource(this, 'Resource', {
      serviceToken: provider.serviceToken,
      resourceType: 'Custom::SnsPlatformApplication',
      properties: {
        Name: props.name,
        Platform: props.platform,
        CredentialSecretArn: props.credentialSecret.secretArn,
        Rotation: props.rotation ?? '',
      },
    });
    this.arn = resource.ref;
  }
}
