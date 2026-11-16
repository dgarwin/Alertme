import 'package:dio/dio.dart';
import 'package:uuid/uuid.dart';

import 'auth.dart';
import 'config.dart';
import 'models.dart';

class ApiException implements Exception {
  final int? status;
  final String message;
  ApiException(this.status, this.message);
  @override
  String toString() => message;
}

/// Client for the AlertMe HTTP API. Attaches the Cognito ID token to every
/// request and retries once with a refreshed token on 401.
class Api {
  Api._() {
    _dio.interceptors.add(InterceptorsWrapper(
      onRequest: (options, handler) async {
        final token = await Auth.instance.idToken();
        if (token != null) options.headers['Authorization'] = 'Bearer $token';
        handler.next(options);
      },
      onError: (e, handler) async {
        final req = e.requestOptions;
        if (e.response?.statusCode == 401 && req.extra['retried'] != true) {
          if (await Auth.instance.restore()) {
            req.extra['retried'] = true;
            try {
              return handler.resolve(await _dio.fetch(req));
            } on DioException catch (e2) {
              return handler.next(e2);
            }
          }
        }
        handler.next(e);
      },
    ));
  }
  static final Api instance = Api._();

  final Dio _dio = Dio(BaseOptions(
    baseUrl: Config.apiBase,
    connectTimeout: const Duration(seconds: 10),
    receiveTimeout: const Duration(seconds: 15),
    contentType: 'application/json',
  ));

  Future<dynamic> _call(String method, String path, {Object? body, Map<String, dynamic>? query}) async {
    try {
      final r = await _dio.request(path,
          data: body, queryParameters: query, options: Options(method: method));
      return r.data;
    } on DioException catch (e) {
      final data = e.response?.data;
      final msg = data is Map && data['error'] != null
          ? data['error'].toString()
          : data is String && data.trim().isNotEmpty
              ? data.trim()
              : (e.message ?? 'Network error');
      throw ApiException(e.response?.statusCode, msg);
    }
  }

  Future<void> registerDevice(String token, String platform) =>
      _call('POST', '/devices', body: {'token': token, 'platform': platform});

  Future<Invite> createInvite() async =>
      Invite.fromJson(await _call('POST', '/invites', body: {}));

  Future<Pairing> acceptInvite(String code) async {
    final r = await _call('POST', '/invites/${Uri.encodeComponent(code.trim())}/accept');
    return Pairing.fromJson(r['pairing']);
  }

  Future<List<Pairing>> pairings() async {
    final r = await _call('GET', '/pairings');
    return [for (final p in (r['pairings'] as List? ?? [])) Pairing.fromJson(p)];
  }

  Future<void> block(String peerId) => _call('POST', '/pairings/$peerId/block');

  Future<Page> sendPage(String recipientId, String message) async {
    final r = await _call('POST', '/pages', body: {
      'recipient_id': recipientId,
      'message': message,
      'idempotency_key': const Uuid().v4(),
    });
    return Page.fromJson(r['page']);
  }

  Future<List<Page>> pages({DateTime? since}) async {
    final r = await _call('GET', '/pages',
        query: since == null ? null : {'since': since.toUtc().toIso8601String()});
    return [for (final p in (r['pages'] as List? ?? [])) Page.fromJson(p)];
  }

  Future<(Page, List<PageEvent>)> page(String id) async {
    final r = await _call('GET', '/pages/$id');
    return (
      Page.fromJson(r['page']),
      [for (final e in (r['events'] as List? ?? [])) PageEvent.fromJson(e)],
    );
  }

  Future<Page> ack(String id, {String? note}) async {
    final r = await _call('POST', '/pages/$id/ack',
        body: {if (note != null && note.isNotEmpty) 'note': note});
    return Page.fromJson(r['page']);
  }

  Future<void> delivered(String id) => _call('POST', '/pages/$id/delivered');
}
