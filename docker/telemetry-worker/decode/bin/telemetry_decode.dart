import 'dart:convert';
import 'dart:io';

import 'package:native_stack_traces/native_stack_traces.dart';

Future<void> main(List<String> args) async {
  if (args.length != 1) {
    stderr.writeln('usage: telemetry-decode <debug-info-file> < stack');
    exit(64);
  }
  final dwarf = Dwarf.fromFile(args.single);
  if (dwarf == null) {
    stderr.writeln('telemetry-decode: no DWARF debug info in ${args.single}');
    exit(65);
  }
  await stdin
      .transform(utf8.decoder)
      .transform(const LineSplitter())
      .transform(DwarfStackTraceDecoder(dwarf))
      .forEach(stdout.writeln);
}
