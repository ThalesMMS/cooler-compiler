class Complex inherits IO {
  x : Int;
  y : Int;
  init(a : Int, b : Int) : Complex { { x <- a; y <- b; self; } };
  print() : Object {
    if y = 0 then out_int(x) else out_int(x).out_string("+").out_int(y).out_string("I") fi
  };
  reflect_0() : Complex { { x <- ~x; y <- ~y; self; } };
  reflect_X() : Complex { { y <- ~y; self; } };
  reflect_Y() : Complex { { x <- ~x; self; } };
  equal(d : Complex) : Bool { if x = d.x_value() then y = d.y_value() else false fi };
  x_value() : Int { x };
  y_value() : Int { y };
};
class Main inherits IO {
  main() : Object {
    let c : Complex <- (new Complex).init(1, 1) in {
      c.print();
      out_string("\n");
      if c.reflect_X().reflect_Y().equal(c.copy().reflect_0())
      then out_string("=)\n") else out_string("=(\n") fi;
      out_string(c.type_name().concat("\n"));
    }
  };
};
