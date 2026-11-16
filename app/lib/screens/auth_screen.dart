import 'package:amazon_cognito_identity_dart_2/cognito.dart';
import 'package:flutter/material.dart';

import '../auth.dart';

/// Email + password sign in / sign up, with email verification code.
class AuthScreen extends StatefulWidget {
  const AuthScreen({super.key, required this.onSignedIn});
  final VoidCallback onSignedIn;

  @override
  State<AuthScreen> createState() => _AuthScreenState();
}

enum _Mode { signIn, signUp, confirm }

class _AuthScreenState extends State<AuthScreen> {
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _code = TextEditingController();
  _Mode _mode = _Mode.signIn;
  bool _busy = false;
  String? _error;
  String? _info;

  Future<void> _run(Future<void> Function() f) async {
    setState(() {
      _busy = true;
      _error = null;
      _info = null;
    });
    try {
      await f();
    } catch (e) {
      if (e is CognitoUserConfirmationNecessaryException) {
        setState(() {
          _mode = _Mode.confirm;
          _info = 'Enter the verification code we emailed you.';
        });
      } else {
        setState(() => _error = authError(e));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  String get _emailValue => _email.text.trim().toLowerCase();

  Future<void> _submit() => _run(() async {
        switch (_mode) {
          case _Mode.signIn:
            await Auth.instance.signIn(_emailValue, _password.text);
            widget.onSignedIn();
          case _Mode.signUp:
            await Auth.instance.signUp(_emailValue, _password.text);
            setState(() {
              _mode = _Mode.confirm;
              _info = 'We emailed a verification code to $_emailValue.';
            });
          case _Mode.confirm:
            await Auth.instance.confirm(_emailValue, _code.text.trim());
            await Auth.instance.signIn(_emailValue, _password.text);
            widget.onSignedIn();
        }
      });

  @override
  Widget build(BuildContext context) {
    final title = switch (_mode) {
      _Mode.signIn => 'Sign in',
      _Mode.signUp => 'Create account',
      _Mode.confirm => 'Verify email',
    };
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: AutofillGroup(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Icon(Icons.notifications_active,
                        size: 64, color: Theme.of(context).colorScheme.primary),
                    const SizedBox(height: 8),
                    Text('AlertMe',
                        textAlign: TextAlign.center,
                        style: Theme.of(context).textTheme.headlineMedium),
                    const SizedBox(height: 24),
                    Text(title, style: Theme.of(context).textTheme.titleLarge),
                    const SizedBox(height: 16),
                    TextField(
                      controller: _email,
                      enabled: _mode != _Mode.confirm,
                      keyboardType: TextInputType.emailAddress,
                      autofillHints: const [AutofillHints.email],
                      decoration: const InputDecoration(labelText: 'Email'),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _password,
                      obscureText: true,
                      autofillHints: [
                        _mode == _Mode.signUp
                            ? AutofillHints.newPassword
                            : AutofillHints.password
                      ],
                      decoration: InputDecoration(
                        labelText: 'Password',
                        helperText: _mode == _Mode.signUp
                            ? '8+ chars with upper, lower, number and symbol'
                            : null,
                      ),
                    ),
                    if (_mode == _Mode.confirm) ...[
                      const SizedBox(height: 12),
                      TextField(
                        controller: _code,
                        keyboardType: TextInputType.number,
                        autofillHints: const [AutofillHints.oneTimeCode],
                        decoration: const InputDecoration(labelText: 'Verification code'),
                      ),
                    ],
                    if (_info != null) ...[
                      const SizedBox(height: 12),
                      Text(_info!),
                    ],
                    if (_error != null) ...[
                      const SizedBox(height: 12),
                      Text(_error!,
                          style: TextStyle(color: Theme.of(context).colorScheme.error)),
                    ],
                    const SizedBox(height: 20),
                    FilledButton(
                      onPressed: _busy ? null : _submit,
                      child: _busy
                          ? const SizedBox(
                              height: 20, width: 20, child: CircularProgressIndicator(strokeWidth: 2))
                          : Text(title),
                    ),
                    const SizedBox(height: 8),
                    if (_mode == _Mode.confirm)
                      TextButton(
                        onPressed: _busy
                            ? null
                            : () => _run(() async {
                                  await Auth.instance.resendCode(_emailValue);
                                  setState(() => _info = 'Code re-sent.');
                                }),
                        child: const Text('Resend code'),
                      ),
                    TextButton(
                      onPressed: _busy
                          ? null
                          : () => setState(() {
                                _error = null;
                                _info = null;
                                _mode = _mode == _Mode.signIn ? _Mode.signUp : _Mode.signIn;
                              }),
                      child: Text(_mode == _Mode.signIn
                          ? 'No account? Create one'
                          : 'Have an account? Sign in'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
