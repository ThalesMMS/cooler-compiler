class Main inherits IO {
  min_int() : Int { (~2147483647) - 1 };

  main() : Object {{
    out_int(2147483647 + 1); out_string("/");
    out_int(min_int() - 1); out_string("/");
    out_int(2147483647 * 2); out_string("/");
    out_int(~min_int()); out_string("/");
    out_int(min_int() / ~1); out_string("/");
    if (2147483647 + 1) < 0 then out_string("T") else out_string("F") fi;
    if (2147483647 + 1) = min_int() then out_string("T") else out_string("F") fi;
    out_string("/"); out_int(min_int()); out_string("/"); out_int(~2147483647);
  }};
};
