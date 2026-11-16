import 'package:amazon_cognito_identity_dart_2/cognito.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'config.dart';

/// Persists Cognito tokens in the platform keystore so sessions survive
/// restarts and are readable from the FCM background isolate.
class SecureCognitoStorage extends CognitoStorage {
  static const _store = FlutterSecureStorage(
    aOptions: AndroidOptions(encryptedSharedPreferences: true),
  );

  @override
  Future<dynamic> getItem(String key) => _store.read(key: key);

  @override
  Future<dynamic> setItem(String key, value) async {
    await _store.write(key: key, value: value?.toString());
    return value;
  }

  @override
  Future<dynamic> removeItem(String key) async {
    final old = await _store.read(key: key);
    await _store.delete(key: key);
    return old;
  }

  @override
  Future<void> clear() => _store.deleteAll();
}

/// Cognito email/password auth (USER_SRP_AUTH, no client secret).
class Auth {
  Auth._();
  static final Auth instance = Auth._();

  final CognitoUserPool _pool = CognitoUserPool(
    Config.cognitoUserPoolId,
    Config.cognitoClientId,
    storage: SecureCognitoStorage(),
  );

  CognitoUser? _user;
  CognitoUserSession? _session;

  CognitoUser _userFor(String email) =>
      CognitoUser(email, _pool, storage: _pool.storage);

  /// Restores a cached session; returns false if the user must sign in.
  Future<bool> restore() async {
    try {
      _user = await _pool.getCurrentUser();
      _session = await _user?.getSession();
      return _session?.isValid() ?? false;
    } catch (_) {
      return false;
    }
  }

  Future<void> signUp(String email, String password) async {
    await _pool.signUp(email, password,
        userAttributes: [AttributeArg(name: 'email', value: email)]);
  }

  Future<void> confirm(String email, String code) async {
    await _userFor(email).confirmRegistration(code);
  }

  Future<void> resendCode(String email) async {
    await _userFor(email).resendConfirmationCode();
  }

  /// Throws [CognitoUserConfirmationNecessaryException] if the email isn't
  /// verified yet.
  Future<void> signIn(String email, String password) async {
    final user = _userFor(email);
    _session = await user.authenticateUser(
        AuthenticationDetails(username: email, password: password));
    _user = user;
  }

  Future<void> signOut() async {
    await _user?.signOut();
    _user = null;
    _session = null;
  }

  /// A valid Cognito ID token (refreshed if expired), or null if signed out.
  /// The API reads the `email` claim, so this must be the ID token.
  Future<String?> idToken() async {
    if (_session == null || !_session!.isValid()) {
      if (!await restore()) return null;
    }
    return _session?.getIdToken().getJwtToken();
  }

  /// The signed-in user's id (Cognito `sub`), as used in sender/recipient ids.
  String? get userId => _session?.getIdToken().payload['sub'] as String?;

  String? get email => _session?.getIdToken().payload['email'] as String?;
}

/// Human-readable message for a Cognito error.
String authError(Object e) {
  if (e is CognitoClientException) return e.message ?? e.code ?? 'Error';
  return e.toString();
}
