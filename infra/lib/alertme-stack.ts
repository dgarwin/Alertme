import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import * as dynamodb from 'aws-cdk-lib/aws-dynamodb';
import * as sqs from 'aws-cdk-lib/aws-sqs';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import * as eventsources from 'aws-cdk-lib/aws-lambda-event-sources';
import * as cognito from 'aws-cdk-lib/aws-cognito';
import * as apigwv2 from 'aws-cdk-lib/aws-apigatewayv2';
import * as integrations from 'aws-cdk-lib/aws-apigatewayv2-integrations';
import * as authorizers from 'aws-cdk-lib/aws-apigatewayv2-authorizers';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as sns from 'aws-cdk-lib/aws-sns';
import * as subscriptions from 'aws-cdk-lib/aws-sns-subscriptions';
import * as cloudwatch from 'aws-cdk-lib/aws-cloudwatch';
import * as cwactions from 'aws-cdk-lib/aws-cloudwatch-actions';
import * as events from 'aws-cdk-lib/aws-events';
import * as targets from 'aws-cdk-lib/aws-events-targets';

// SSM parameter (SecureString, created out-of-band — see README) holding the
// FCM service-account JSON used by the pager lambda to mint push tokens.
const FCM_SA_PARAM = '/alertme/fcm-service-account';

export class AlertmeStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    const alertEmail: string = this.node.tryGetContext('alertEmail') ?? '';
    const replicaRegions: string[] = this.node.tryGetContext('replicaRegions') ?? [];
    const canaryEnabled: boolean = this.node.tryGetContext('canaryEnabled') ?? false;

    // ------------------------------------------------------------------ data
    // Single-table design (see PLAN-AWS.md §3). GSI1 serves recipient inbox,
    // history, and GET /pages?since polling. GSI2 mirrors it for the sender
    // side (ListPagesFor queries both and merges) so GET /pages?since also
    // returns pages the caller sent, not just ones they received.
    const table = new dynamodb.TableV2(this, 'Table', {
      tableName: 'alertme',
      partitionKey: { name: 'PK', type: dynamodb.AttributeType.STRING },
      sortKey: { name: 'SK', type: dynamodb.AttributeType.STRING },
      billing: dynamodb.Billing.onDemand(),
      pointInTimeRecoverySpecification: { pointInTimeRecoveryEnabled: true },
      timeToLiveAttribute: 'ttl',
      globalSecondaryIndexes: [
        {
          indexName: 'GSI1',
          partitionKey: { name: 'GSI1PK', type: dynamodb.AttributeType.STRING },
          sortKey: { name: 'GSI1SK', type: dynamodb.AttributeType.STRING },
        },
        {
          indexName: 'GSI2',
          partitionKey: { name: 'GSI2PK', type: dynamodb.AttributeType.STRING },
          sortKey: { name: 'GSI2SK', type: dynamodb.AttributeType.STRING },
        },
      ],
      replicas: replicaRegions.map((region) => ({ region })),
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });

    // ------------------------------------------------------------- nag queue
    // The nag scheduler: each attempt re-enqueues the next with DelaySeconds.
    // Poison messages land in the DLQ and page a human via the alarm below.
    const dlq = new sqs.Queue(this, 'PageDlq', {
      queueName: 'alertme-pages-dlq',
      retentionPeriod: cdk.Duration.days(14),
    });
    const queue = new sqs.Queue(this, 'PageQueue', {
      queueName: 'alertme-pages',
      visibilityTimeout: cdk.Duration.seconds(60),
      deadLetterQueue: { queue: dlq, maxReceiveCount: 5 },
    });

    // ----------------------------------------------------------------- auth
    const userPool = new cognito.UserPool(this, 'Users', {
      userPoolName: 'alertme',
      selfSignUpEnabled: true,
      signInAliases: { email: true },
      accountRecovery: cognito.AccountRecovery.EMAIL_ONLY,
      removalPolicy: cdk.RemovalPolicy.RETAIN,
      // TODO(launch): add Apple + Google as federated identity providers
      // (cognito.UserPoolIdentityProviderApple / ...Google). Requires Apple
      // Developer + Google Cloud OAuth credentials; email sign-in works for dev.
    });
    const userPoolClient = userPool.addClient('AppClient', {
      authFlows: { userSrp: true },
      oAuth: {
        flows: { authorizationCodeGrant: true },
        callbackUrls: ['alertme://auth/callback'],
        logoutUrls: ['alertme://auth/signout'],
      },
      preventUserExistenceErrors: true,
    });
    userPool.addDomain('Domain', {
      cognitoDomain: { domainPrefix: `alertme-${this.account}` },
    });

    // -------------------------------------------------------------- lambdas
    // Go binaries: `make -C ../backend build` produces dist/<name>/bootstrap
    // (linux/arm64) before synth/deploy. Both run outside any VPC by design.
    const goFn = (name: string, extraEnv: Record<string, string> = {}) =>
      new lambda.Function(this, `${name}Fn`, {
        functionName: `alertme-${name.toLowerCase()}`,
        runtime: lambda.Runtime.PROVIDED_AL2023,
        architecture: lambda.Architecture.ARM_64,
        handler: 'bootstrap',
        code: lambda.Code.fromAsset(`../backend/dist/${name.toLowerCase()}`),
        memorySize: 128,
        timeout: cdk.Duration.seconds(30),
        environment: {
          TABLE_NAME: table.tableName,
          GSI1_NAME: 'GSI1',
          GSI2_NAME: 'GSI2',
          QUEUE_URL: queue.queueUrl,
          FCM_SA_PARAM,
          ...extraEnv,
        },
      });

    const apiFn = goFn('Api');
    const pagerFn = goFn('Pager');
    const canaryFn = goFn('Canary');

    table.grantReadWriteData(apiFn);
    table.grantReadWriteData(pagerFn);
    queue.grantSendMessages(apiFn); // page creation enqueues attempt 0
    queue.grantSendMessages(pagerFn); // each attempt enqueues the next
    pagerFn.addEventSource(
      new eventsources.SqsEventSource(queue, {
        batchSize: 5,
        reportBatchItemFailures: true,
      })
    );
    pagerFn.addToRolePolicy(
      new iam.PolicyStatement({
        actions: ['ssm:GetParameter'],
        resources: [
          `arn:aws:ssm:${this.region}:${this.account}:parameter${FCM_SA_PARAM}`,
        ],
      })
    );

    // ------------------------------------------------------------------ api
    const httpApi = new apigwv2.HttpApi(this, 'HttpApi', {
      apiName: 'alertme',
      // CORS is irrelevant for the native app but harmless for tooling.
    });
    const authorizer = new authorizers.HttpUserPoolAuthorizer('Jwt', userPool, {
      userPoolClients: [userPoolClient],
    });
    const apiIntegration = new integrations.HttpLambdaIntegration('ApiInt', apiFn);
    httpApi.addRoutes({
      path: '/{proxy+}',
      methods: [apigwv2.HttpMethod.ANY],
      integration: apiIntegration,
      authorizer,
    });
    httpApi.addRoutes({
      path: '/health',
      methods: [apigwv2.HttpMethod.GET],
      integration: apiIntegration, // unauthenticated; used by Route 53 checks
    });

    // --------------------------------------------------------------- canary
    // Bot A pages bot B through the real API every 5 minutes and pings the
    // dead-man URL only on success. Enable via context once implemented.
    new events.Rule(this, 'CanarySchedule', {
      schedule: events.Schedule.rate(cdk.Duration.minutes(5)),
      targets: [new targets.LambdaFunction(canaryFn)],
      enabled: canaryEnabled,
    });
    canaryFn.addEnvironment('API_BASE_URL', httpApi.apiEndpoint);
    canaryFn.addEnvironment('COGNITO_CLIENT_ID', userPoolClient.userPoolClientId);
    // Bot credentials (BOT_A_USERNAME/PASSWORD, BOT_B_USERNAME/PASSWORD,
    // BOT_B_USER_ID) and HEARTBEAT_URL are deliberately not set here — the
    // canary logs and no-ops until they're provisioned out of band (see
    // cmd/canary's header comment) and added via `canaryFn.addEnvironment`
    // or the console once bot accounts exist.
    userPool.grant(canaryFn, 'cognito-idp:InitiateAuth');

    // --------------------------------------------------------------- alarms
    const alarmTopic = new sns.Topic(this, 'Alarms', { topicName: 'alertme-alarms' });
    if (alertEmail) {
      alarmTopic.addSubscription(new subscriptions.EmailSubscription(alertEmail));
    }
    const alarm = (id: string, metric: cloudwatch.Metric, threshold: number) => {
      const a = new cloudwatch.Alarm(this, id, {
        metric,
        threshold,
        evaluationPeriods: 1,
        comparisonOperator:
          cloudwatch.ComparisonOperator.GREATER_THAN_OR_EQUAL_TO_THRESHOLD,
        treatMissingData: cloudwatch.TreatMissingData.NOT_BREACHING,
      });
      a.addAlarmAction(new cwactions.SnsAction(alarmTopic));
      return a;
    };
    alarm('DlqAlarm', dlq.metricApproximateNumberOfMessagesVisible(), 1);
    alarm('PagerErrors', pagerFn.metricErrors({ period: cdk.Duration.minutes(5) }), 1);
    alarm('Api5xx', httpApi.metricServerError({ period: cdk.Duration.minutes(5) }), 5);

    // -------------------------------------------------------------- outputs
    new cdk.CfnOutput(this, 'ApiUrl', { value: httpApi.apiEndpoint });
    new cdk.CfnOutput(this, 'TableName', { value: table.tableName });
    new cdk.CfnOutput(this, 'QueueUrl', { value: queue.queueUrl });
    new cdk.CfnOutput(this, 'UserPoolId', { value: userPool.userPoolId });
    new cdk.CfnOutput(this, 'UserPoolClientId', {
      value: userPoolClient.userPoolClientId,
    });
  }
}
