/// Direction/outcome of a recorded call, modeled after typical phone call
/// logs.
enum CallType {
  outgoing,
  incoming,
  missed,
  rejected;

  static CallType fromName(String? name) {
    return CallType.values.firstWhere(
      (t) => t.name == name,
      orElse: () => CallType.incoming,
    );
  }
}

/// A single entry in the persistent call history.
class CallLogEntry {
  final String id;
  CallType type;
  String number; // canonical (digits-only full number, or service code)
  String? nameSnapshot; // server-provided caller display name (incoming)
  String? serviceCode;
  String? serviceName;
  DateTime startedAt;
  DateTime? connectedAt;
  DateTime? endedAt;
  int durationSec;

  CallLogEntry({
    required this.id,
    required this.type,
    required this.number,
    this.nameSnapshot,
    this.serviceCode,
    this.serviceName,
    required this.startedAt,
    this.connectedAt,
    this.endedAt,
    this.durationSec = 0,
  });

  bool get connected => connectedAt != null;

  Map<String, dynamic> toJson() => {
    'id': id,
    'type': type.name,
    'number': number,
    'name': nameSnapshot,
    'service_code': serviceCode,
    'service_name': serviceName,
    'started_at': startedAt.toIso8601String(),
    'connected_at': connectedAt?.toIso8601String(),
    'ended_at': endedAt?.toIso8601String(),
    'duration_sec': durationSec,
  };

  factory CallLogEntry.fromJson(Map<String, dynamic> json) => CallLogEntry(
    id: json['id'] as String,
    type: CallType.fromName(json['type'] as String?),
    number: json['number'] as String,
    nameSnapshot: json['name'] as String?,
    serviceCode: json['service_code'] as String?,
    serviceName: json['service_name'] as String?,
    startedAt: DateTime.parse(json['started_at'] as String),
    connectedAt: json['connected_at'] == null
        ? null
        : DateTime.parse(json['connected_at'] as String),
    endedAt: json['ended_at'] == null
        ? null
        : DateTime.parse(json['ended_at'] as String),
    durationSec: json['duration_sec'] as int? ?? 0,
  );
}
