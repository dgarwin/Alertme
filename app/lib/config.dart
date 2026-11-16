/// Static backend configuration. Not secret — safe to commit.
/// Values come from the live us-east-2 deployment (see BUILD-SPEC.md).
class Config {
  static const String apiBase =
      'https://x7qtg546oa.execute-api.us-east-2.amazonaws.com';

  static const String cognitoRegion = 'us-east-2';
  static const String cognitoUserPoolId = 'us-east-2_2L2LUi3h6';
  static const String cognitoClientId = '3qfmo7gu8ouim595dfp32qljju';
}
