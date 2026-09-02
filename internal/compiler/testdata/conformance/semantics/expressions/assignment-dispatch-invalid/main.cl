class Parent {
  method(x : Int) : SELF_TYPE { self };
};
class Other {};
class Main {
  assignments() : Object {{
    self <- 1;
    missing <- 1;
    let x : Int in x <- "bad";
  }};
  dispatches() : Object {{
    self@SELF_TYPE.method(1);
    self@Missing.method(1);
    (new Other)@Parent.method(1);
    (new Parent).missing();
    (new Parent).method();
    (new Parent).method("bad");
  }};
  main() : Object { 0 };
};
