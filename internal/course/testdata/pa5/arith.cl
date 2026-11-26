class Main inherits IO {
  show(label : String, value : Int) : Object {
    { out_string(label); out_string(" = "); out_int(value); out_string("\n"); }
  };
  main() : Object {
    let a : Int <- 17, b : Int <- 5 in {
      show("a + b", a + b);
      show("a - b", a - b);
      show("a * b", a * b);
      show("a / b", a / b);
      show("~a", ~a);
      show("(a * ~b) / 3", (a * ~b) / 3);
      if a < b then out_string("lt\n") else out_string("ge\n") fi;
      if b <= 5 then out_string("le\n") else out_string("gt\n") fi;
      if a = 17 then out_string("eq\n") else out_string("ne\n") fi;
      if not (a = b) then out_string("not eq\n") else out_string("eq\n") fi;
    }
  };
};
