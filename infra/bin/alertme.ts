#!/usr/bin/env node
import * as cdk from 'aws-cdk-lib';
import { AlertmeStack } from '../lib/alertme-stack';

const app = new cdk.App();

// Region comes from the standard CDK env vars / --profile. The stack is
// region-agnostic by construction; deploy it to a second region for the warm
// standby (the DynamoDB global table replica is configured via the
// "replicaRegions" context on the primary stack only).
new AlertmeStack(app, 'Alertme', {
  env: {
    account: process.env.CDK_DEFAULT_ACCOUNT,
    region: process.env.CDK_DEFAULT_REGION,
  },
  description: 'AlertMe paging app: API, pager worker, DynamoDB, Cognito, SQS',
});
